//go:build integration

package integration_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	postgres "sweetshop/internal/repository/postgres"
)

// fixtureTables are the tables 002_demo_seed writes.
var fixtureTables = []string{"users", "categories", "manufacturers", "brands", "products", "collections", "collection_products", "cart_items", "favorites", "orders", "order_items"}

// assertNoBusinessRows proves a production database holds no demo data:
// every table except migration metadata is empty, the fixture tables exist,
// and none of the fixture's identities is present.
func (d *database) assertNoBusinessRows(t *testing.T) {
	t.Helper()
	rows, err := d.pool.Query(d.ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename<>'schema_migrations' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	rows.Close()
	for _, required := range fixtureTables {
		if i := sort.SearchStrings(tables, required); i == len(tables) || tables[i] != required {
			t.Fatalf("fixture table %s missing from schema: %v", required, tables)
		}
	}
	for _, table := range tables {
		if n := d.number(t, "SELECT count(*) FROM "+table); n != 0 {
			t.Errorf("%s has %d rows", table, n)
		}
	}
	for _, probe := range []string{
		"SELECT count(*) FROM users WHERE id='usr-demo' OR lower(email)='demo@sugar.ru'",
		"SELECT count(*) FROM products WHERE id IN ('p01','p24') OR sku='CHA-701'",
		"SELECT count(*) FROM brands WHERE id='brand-unbranded'",
		"SELECT count(*) FROM orders WHERE id='ord-2026-002' OR number='#10482'",
	} {
		if d.number(t, probe) != 0 {
			t.Errorf("fixture identity present: %s", probe)
		}
	}
}

func (d *database) outcomes(t *testing.T) string {
	t.Helper()
	return d.text(t, `SELECT string_agg(version||'='||outcome, ',' ORDER BY version) FROM schema_migrations`)
}

const allApplied = "001_init=applied,002_demo_seed=applied,003_catalog_scale_indexes=applied,004_brand_manufacturer_integrity=applied,005_session_transport=applied,006_order_idempotency=applied,007_repeat_commits=applied,008_customer_account_status=applied,009_staff_identity=applied,010_customer_self_registration=applied"
const productionOutcomes = "001_init=applied,002_demo_seed=skipped,003_catalog_scale_indexes=applied,004_brand_manufacturer_integrity=applied,005_session_transport=applied,006_order_idempotency=applied,007_repeat_commits=applied,008_customer_account_status=applied,009_staff_identity=applied,010_customer_self_registration=applied"

// productionDatabase migrates a fresh database the way APP_MODE=production does.
func productionDatabase(t *testing.T) *database {
	t.Helper()
	d := newDatabase(t, false)
	if err := productionMigrator("../../migrations", &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFixtureApplyFreshDatabase(t *testing.T) {
	d := newDatabase(t, false)
	if err := migrator("../../migrations", &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if d.outcomes(t) != allApplied {
		t.Fatal(d.outcomes(t))
	}
	d.assertChecksumsMatchRepository(t)
	for table, want := range map[string]int64{"users": 1, "categories": 10, "manufacturers": 8, "brands": 12, "products": 24, "collections": 4, "collection_products": 18, "cart_items": 4, "favorites": 5, "orders": 1, "order_items": 4} {
		if got := d.number(t, "SELECT count(*) FROM "+table); got != want {
			t.Errorf("%s=%d want %d", table, got, want)
		}
	}
	if err := migrator("../../migrations", &lockedBuffer{}).Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("apply verify on applied: %v", err)
	}
	err := productionMigrator("../../migrations", &lockedBuffer{}).Verify(d.ctx, d.pool)
	if !errors.Is(err, postgres.ErrDemoFixturesApplied) || errors.Is(err, postgres.ErrSchemaNotCurrent) || strings.Contains(err.Error(), "shopctl migrate") {
		t.Fatalf("production verify on applied: %v", err)
	}
	// Production migrate refuses too, and never deletes or rewrites anything.
	if err := productionMigrator("../../migrations", &lockedBuffer{}).Migrate(d.ctx, d.pool); !errors.Is(err, postgres.ErrDemoFixturesApplied) {
		t.Fatalf("production migrate on applied: %v", err)
	}
	if d.outcomes(t) != allApplied || d.number(t, "SELECT count(*) FROM products") != 24 {
		t.Fatal("production policy changed a demo database")
	}
	status, err := migrator("../../migrations", &lockedBuffer{}).Status(d.ctx, d.pool)
	if err != nil || status.Migrations[1].State != postgres.MigrationApplied || !status.Migrations[1].Fixture || status.Migrations[0].Fixture {
		t.Fatalf("status: %+v %v", status.Migrations, err)
	}
}

func TestFixtureSkipFreshProductionDatabase(t *testing.T) {
	d := newDatabase(t, false)
	var logs lockedBuffer
	prod := productionMigrator("../../migrations", &logs)
	status, err := prod.Status(d.ctx, d.pool)
	if err != nil || status.Migrations[1].State != postgres.MigrationPendingSkip || status.Migrations[0].State != postgres.MigrationPending {
		t.Fatalf("fresh status: %+v %v", status.Migrations, err)
	}
	if err := prod.Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if d.outcomes(t) != productionOutcomes {
		t.Fatal(d.outcomes(t))
	}
	// A skipped fixture still records the real file's checksum.
	d.assertRecordedChecksums(t, "skipped")
	d.assertNoBusinessRows(t)
	if strings.Count(logs.String(), `msg="fixture migration skipped"`) != 1 || strings.Count(logs.String(), `msg="migration applied"`) != repositoryMigrations(t)-1 {
		t.Fatalf("logs: %s", logs.String())
	}

	// Structurally the same schema as a demo database, with empty tables.
	demo := newDatabase(t, true)
	if d.text(t, schemaFingerprint) != demo.text(t, schemaFingerprint) {
		t.Fatal("production schema differs from the fixture schema")
	}
	sequence := `SELECT last_value||'/'||is_called FROM order_number_seq`
	if d.text(t, sequence) != demo.text(t, sequence) {
		t.Fatal("order number sequence differs")
	}

	if err := prod.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("production verify on skipped: %v", err)
	}
	dev := migrator("../../migrations", &lockedBuffer{})
	err = dev.Verify(d.ctx, d.pool)
	if !errors.Is(err, postgres.ErrFixturePolicyConflict) || errors.Is(err, postgres.ErrDemoFixturesApplied) || errors.Is(err, postgres.ErrSchemaNotCurrent) {
		t.Fatalf("apply verify on skipped: %v", err)
	}
	// A development migrator cannot turn a production database into a demo one.
	if err := dev.Migrate(d.ctx, d.pool); !errors.Is(err, postgres.ErrFixturePolicyConflict) {
		t.Fatalf("apply migrate on skipped: %v", err)
	}
	if d.outcomes(t) != productionOutcomes {
		t.Fatal("outcome flipped")
	}
	d.assertNoBusinessRows(t)

	// Status under either policy is read-only and truthful.
	before := d.text(t, `SELECT string_agg(version||checksum||outcome||applied_at::text, ',' ORDER BY version) FROM schema_migrations`)
	status, err = prod.Status(d.ctx, d.pool)
	if err != nil || !status.Current() || status.Migrations[1].State != postgres.MigrationSkipped || !status.Migrations[1].Fixture {
		t.Fatalf("status: %+v %v", status, err)
	}
	if _, err := dev.Status(d.ctx, d.pool); !errors.Is(err, postgres.ErrFixturePolicyConflict) {
		t.Fatalf("dev status on skipped: %v", err)
	}
	var again lockedBuffer
	if err := productionMigrator("../../migrations", &again).Migrate(d.ctx, d.pool); err != nil || strings.Contains(again.String(), `msg="fixture migration skipped"`) {
		t.Fatalf("rerun: %v %s", err, again.String())
	}
	if d.text(t, `SELECT string_agg(version||checksum||outcome||applied_at::text, ',' ORDER BY version) FROM schema_migrations`) != before {
		t.Fatal("status or rerun changed metadata")
	}
}

func TestMigratorWithoutPolicyRunsNothing(t *testing.T) {
	d := newDatabase(t, false)
	m := postgres.Migrator{Directory: "../../migrations"}
	if err := m.Migrate(d.ctx, d.pool); err == nil {
		t.Fatal("migrate without a policy")
	}
	if _, err := m.Status(d.ctx, d.pool); err == nil {
		t.Fatal("status without a policy")
	}
	if err := m.Verify(d.ctx, d.pool); err == nil {
		t.Fatal("verify without a policy")
	}
	if d.number(t, "SELECT count(*) FROM pg_class WHERE relname IN ('schema_migrations','users')") != 0 {
		t.Fatal("SQL ran without a policy")
	}
}

func TestSkippedFixtureChecksumStillEnforced(t *testing.T) {
	d := productionDatabase(t)
	edited := migrationDir(t, nil, nil)
	writeFile(t, edited, "002_demo_seed.up.sql", "-- edited fixture\n")
	for name, m := range map[string]postgres.Migrator{"production": productionMigrator(edited, &lockedBuffer{}), "development": migrator(edited, &lockedBuffer{})} {
		if err := m.Migrate(d.ctx, d.pool); err == nil || !strings.Contains(err.Error(), "fixture migration 002_demo_seed checksum") {
			t.Fatalf("%s migrate with edited fixture: %v", name, err)
		}
		if err := m.Verify(d.ctx, d.pool); err == nil {
			t.Fatalf("%s verify accepted edited fixture", name)
		}
	}
	d.exec(t, "UPDATE schema_migrations SET checksum=repeat('0',64) WHERE version='002_demo_seed'")
	prod := productionMigrator("../../migrations", &lockedBuffer{})
	if err := prod.Verify(d.ctx, d.pool); err == nil || errors.Is(err, postgres.ErrSchemaNotCurrent) || !strings.Contains(err.Error(), "002_demo_seed checksum mismatch") {
		t.Fatalf("recorded skipped checksum mismatch: %v", err)
	}
	if err := prod.Migrate(d.ctx, d.pool); err == nil {
		t.Fatal("migrate accepted a mismatched skipped checksum")
	}
	if d.text(t, "SELECT outcome||'/'||checksum FROM schema_migrations WHERE version='002_demo_seed'") != "skipped/"+strings.Repeat("0", 64) {
		t.Fatal("skipped row rewritten")
	}
}

func TestMissingFixtureIsAGapNotASkip(t *testing.T) {
	d := newDatabase(t, false)
	withoutFixture := migrationDir(t, []string{"001_", "003_", "004_", "005_", "006_", "007_"}, nil)
	if err := productionMigrator(withoutFixture, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]postgres.Migrator{"production": productionMigrator("../../migrations", &lockedBuffer{}), "development": migrator("../../migrations", &lockedBuffer{})} {
		if err := m.Migrate(d.ctx, d.pool); err == nil || !strings.Contains(err.Error(), "migration 002_demo_seed is pending but later migration 007_repeat_commits is already applied") {
			t.Fatalf("%s: %v", name, err)
		}
		if err := m.Verify(d.ctx, d.pool); err == nil || errors.Is(err, postgres.ErrSchemaNotCurrent) {
			t.Fatalf("%s verify: %v", name, err)
		}
	}
	if d.number(t, "SELECT count(*) FROM schema_migrations WHERE version='002_demo_seed'") != 0 {
		t.Fatal("fixture filled in under later versions")
	}
}

func TestConcurrentProductionMigratorsSkipFixtureOnce(t *testing.T) {
	d := newDatabase(t, false)
	other, err := pgxpool.NewWithConfig(d.ctx, d.pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	release := d.holdMigrationLock(t)
	logs := []*lockedBuffer{{}, {}}
	errs := make(chan error, 2)
	for i, pool := range []*pgxpool.Pool{d.pool, other} {
		go func(m postgres.Migrator, pool *pgxpool.Pool) { errs <- m.Migrate(d.ctx, pool) }(productionMigrator("../../migrations", logs[i]), pool)
	}
	waitFor(t, "both production migrators to wait", func() bool {
		return strings.Contains(logs[0].String(), "waiting for the migration lock") && strings.Contains(logs[1].String(), "waiting for the migration lock")
	})
	release()
	for range logs {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if d.outcomes(t) != productionOutcomes {
		t.Fatal(d.outcomes(t))
	}
	d.assertNoBusinessRows(t)
	skips := []int{strings.Count(logs[0].String(), `msg="fixture migration skipped"`), strings.Count(logs[1].String(), `msg="fixture migration skipped"`)}
	applied := []int{strings.Count(logs[0].String(), `msg="migration applied"`), strings.Count(logs[1].String(), `msg="migration applied"`)}
	sort.Ints(skips)
	sort.Ints(applied)
	if !reflect.DeepEqual(skips, []int{0, 1}) || !reflect.DeepEqual(applied, []int{0, repositoryMigrations(t) - 1}) {
		t.Fatalf("skips=%v applied=%v", skips, applied)
	}
	if !d.migrationLockFree(t) {
		t.Fatal("migration lock still held")
	}
}

func TestFailureAfterSkippedFixture(t *testing.T) {
	d := newDatabase(t, false)
	dir := migrationDir(t, nil, map[string]string{"900_late.up.sql": "CREATE TABLE late_probe(id int); SELECT deliberately_missing_function();"})
	if err := productionMigrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err == nil {
		t.Fatal("failing migration succeeded")
	}
	if d.outcomes(t) != productionOutcomes || d.number(t, "SELECT count(*) FROM pg_class WHERE relname='late_probe'") != 0 {
		t.Fatalf("after failure: %s", d.outcomes(t))
	}
	d.assertNoBusinessRows(t)
	writeFile(t, dir, "900_late.up.sql", "CREATE TABLE late_probe(id int);")
	if err := productionMigrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if d.outcomes(t) != productionOutcomes+",900_late=applied" {
		t.Fatal(d.outcomes(t))
	}
}

func TestLegacyFixtureDatabaseKeepsWorkingInDevelopmentOnly(t *testing.T) {
	d := newDatabase(t, false)
	if err := legacyMigrate(d.ctx, d.pool, historicalDir(t)); err != nil {
		t.Fatal(err)
	}
	prod := productionMigrator("../../migrations", &lockedBuffer{})
	if err := prod.Migrate(d.ctx, d.pool); !errors.Is(err, postgres.ErrDemoFixturesApplied) {
		t.Fatalf("production on legacy demo database: %v", err)
	}
	var logs lockedBuffer
	if err := migrator("../../migrations", &logs).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if strings.Count(logs.String(), `msg="migration checksum adopted"`) != 7 || d.outcomes(t) != allApplied {
		t.Fatalf("adoption: %s %s", d.outcomes(t), logs.String())
	}
	d.assertChecksumsMatchRepository(t)
	if err := prod.Verify(d.ctx, d.pool); !errors.Is(err, postgres.ErrDemoFixturesApplied) {
		t.Fatalf("production verify on adopted demo database: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM products") != 24 || d.outcomes(t) != allApplied {
		t.Fatal("refusal modified the database")
	}
}

func TestA1MetadataUpgradesToSkippedOutcome(t *testing.T) {
	d := newDatabase(t, false)
	if err := legacyMigrate(d.ctx, d.pool, historicalDir(t)); err != nil {
		t.Fatal(err)
	}
	// Exactly what the A1 binary wrote: checksums and an applied-only outcome.
	d.exec(t, `ALTER TABLE schema_migrations
		ADD COLUMN checksum text CONSTRAINT schema_migrations_checksum_format CHECK (checksum ~ '^[0-9a-f]{64}$'),
		ADD COLUMN outcome text NOT NULL DEFAULT 'applied' CONSTRAINT schema_migrations_outcome_known CHECK (outcome IN ('applied'))`)
	for _, version := range []string{"001_init", "002_demo_seed", "003_catalog_scale_indexes", "004_brand_manufacturer_integrity", "005_session_transport", "006_order_idempotency", "007_repeat_commits"} {
		d.exec(t, "UPDATE schema_migrations SET checksum=$2 WHERE version=$1", version, fileChecksum(t, filepath.Join("../../migrations", version+".up.sql")))
	}
	m := migrator("../../migrations", &lockedBuffer{})
	status, err := m.Status(d.ctx, d.pool)
	if err != nil || status.MetadataCurrent || status.Migrations[0].State != postgres.MigrationApplied {
		t.Fatalf("A1 metadata status must read real checksums: %+v %v", status, err)
	}
	if err := m.Verify(d.ctx, d.pool); !errors.Is(err, postgres.ErrSchemaNotCurrent) {
		t.Fatalf("A1 metadata verify: %v", err)
	}
	var logs lockedBuffer
	if err := migrator("../../migrations", &logs).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "migration metadata bootstrapped") || strings.Contains(logs.String(), "adopted\"") {
		t.Fatalf("logs: %s", logs.String())
	}
	constraints := `SELECT string_agg(conname, ',' ORDER BY conname) FROM pg_constraint WHERE conrelid='schema_migrations'::regclass AND contype='c'`
	if d.text(t, constraints) != "schema_migrations_checksum_format,schema_migrations_outcome_allowed" {
		t.Fatal(d.text(t, constraints))
	}
	if err := m.Verify(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	// Earlier binaries' insert shape still works; unknown outcomes are refused.
	d.exec(t, "INSERT INTO schema_migrations(version) VALUES ('900_previous_binary_shape')")
	if _, err := d.pool.Exec(d.ctx, "INSERT INTO schema_migrations(version,outcome) VALUES ('901_x','purged')"); err == nil {
		t.Fatal("unknown outcome accepted by metadata")
	}
}

func TestPreviousRunnerLeavesSkippedFixtureAlone(t *testing.T) {
	d := productionDatabase(t)
	// The pre-A1 runner checks only that a version row exists.
	if err := legacyMigrate(d.ctx, d.pool, historicalDir(t)); err != nil {
		t.Fatal(err)
	}
	if d.outcomes(t) != productionOutcomes {
		t.Fatal(d.outcomes(t))
	}
	d.assertNoBusinessRows(t)
}

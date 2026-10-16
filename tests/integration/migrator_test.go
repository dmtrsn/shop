//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	postgres "sweetshop/internal/repository/postgres"
)

// legacyMigrate is the runner of the previous binary (commit 4556694),
// verbatim. It proves that binary still works against A1 metadata.
func legacyMigrate(ctx context.Context, pool *pgxpool.Pool, directory string) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read migrations directory %q: %w", directory, err)
	}
	files := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		version := strings.TrimSuffix(name, ".up.sql")
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists {
			continue
		}
		body, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		quotedVersion := strings.ReplaceAll(version, "'", "''")
		batch := "BEGIN;\n" + string(body) + "\nINSERT INTO schema_migrations(version) VALUES ('" + quotedVersion + "');\nCOMMIT;"
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return err
		}
		_, execErr := conn.Conn().PgConn().Exec(ctx, batch).ReadAll()
		conn.Release()
		if execErr != nil {
			return fmt.Errorf("apply migration %s: %w", name, execErr)
		}
	}
	return nil
}

// lockedBuffer is a log sink shared with a migrator goroutine.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func migrator(dir string, logs *lockedBuffer) postgres.Migrator {
	return postgres.Migrator{Directory: dir, Logger: slog.New(slog.NewTextHandler(logs, nil))}
}

// migrationDir copies repository migrations (all, or those with the given
// prefixes) into a temporary directory and adds extra files.
func migrationDir(t *testing.T, prefixes []string, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		keep := prefixes == nil
		for _, prefix := range prefixes {
			keep = keep || strings.HasPrefix(entry.Name(), prefix)
		}
		if !keep {
			continue
		}
		body, err := os.ReadFile(filepath.Join("../../migrations", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, entry.Name(), string(body))
	}
	for name, body := range extra {
		writeFile(t, dir, name, body)
	}
	return dir
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileChecksum(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (d *database) text(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var value string
	if err := d.pool.QueryRow(d.ctx, sql, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (d *database) assertChecksumsMatchRepository(t *testing.T) {
	t.Helper()
	rows, err := d.pool.Query(d.ctx, `SELECT version,checksum,outcome FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var version, outcome string
		var checksum *string
		if err := rows.Scan(&version, &checksum, &outcome); err != nil {
			t.Fatal(err)
		}
		count++
		want := fileChecksum(t, filepath.Join("../../migrations", version+".up.sql"))
		if checksum == nil || *checksum != want || outcome != "applied" {
			t.Errorf("%s checksum=%v outcome=%s", version, checksum, outcome)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Fatalf("schema_migrations rows=%d", count)
	}
}

// migrationLockFree reports whether another session can take the migration lock.
func (d *database) migrationLockFree(t *testing.T) bool {
	t.Helper()
	conn, err := d.pool.Acquire(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(d.ctx, `SELECT pg_try_advisory_lock($1)`, postgres.MigrationLockKey).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	if locked {
		if _, err := conn.Exec(d.ctx, `SELECT pg_advisory_unlock($1)`, postgres.MigrationLockKey); err != nil {
			t.Fatal(err)
		}
	}
	return locked
}

// holdMigrationLock takes the migration lock in a test session until release.
func (d *database) holdMigrationLock(t *testing.T) (release func()) {
	t.Helper()
	conn, err := d.pool.Acquire(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(d.ctx, `SELECT pg_advisory_lock($1)`, postgres.MigrationLockKey); err != nil {
		conn.Release()
		t.Fatal(err)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			if _, err := conn.Exec(d.ctx, `SELECT pg_advisory_unlock($1)`, postgres.MigrationLockKey); err != nil {
				t.Error(err)
			}
			conn.Release()
		})
	}
	t.Cleanup(release)
	return release
}

func waitFor(t *testing.T, label string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMigratorFreshDatabase(t *testing.T) {
	d := newDatabase(t, false)
	var logs lockedBuffer
	m := migrator("../../migrations", &logs)
	status, err := m.Status(d.ctx, d.pool)
	if err != nil || status.MetadataCurrent || status.Current() || len(status.Migrations) != 7 || status.Migrations[0].State != postgres.MigrationPending {
		t.Fatalf("fresh status: %+v %v", status, err)
	}
	if err := m.Verify(d.ctx, d.pool); !errors.Is(err, postgres.ErrSchemaNotCurrent) {
		t.Fatalf("fresh verify: %v", err)
	}
	if d.number(t, `SELECT count(*) FROM pg_class WHERE relname='schema_migrations'`) != 0 {
		t.Fatal("status/verify created metadata")
	}
	if err := m.Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	d.assertChecksumsMatchRepository(t)
	if err := m.Verify(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if status, err := m.Status(d.ctx, d.pool); err != nil || !status.Current() || len(status.Newer) != 0 {
		t.Fatalf("migrated status: %+v %v", status, err)
	}
	if strings.Count(logs.String(), "msg=\"migration applied\"") != 7 || strings.Contains(logs.String(), "INSERT INTO") {
		t.Fatalf("logs: %s", logs.String())
	}
	if !d.migrationLockFree(t) {
		t.Fatal("migration lock still held")
	}
}

func TestMigratorAdoptsLegacyMetadataWithoutRerunning(t *testing.T) {
	d := newDatabase(t, false)
	if err := legacyMigrate(d.ctx, d.pool, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	newColumns := `SELECT count(*) FROM pg_attribute WHERE attrelid='schema_migrations'::regclass AND attname IN ('checksum','outcome')`
	m := migrator("../../migrations", &lockedBuffer{})
	status, err := m.Status(d.ctx, d.pool)
	if err != nil || status.MetadataCurrent || status.Migrations[6].State != postgres.MigrationChecksumUnrecorded {
		t.Fatalf("legacy status: %+v %v", status, err)
	}
	if err := m.Verify(d.ctx, d.pool); !errors.Is(err, postgres.ErrSchemaNotCurrent) {
		t.Fatalf("legacy verify: %v", err)
	}
	if d.number(t, newColumns) != 0 {
		t.Fatal("status/verify changed legacy metadata")
	}

	// Live data the fixture seeded; any rerun would reset or duplicate it.
	d.exec(t, "UPDATE products SET stock=23 WHERE id='p01'")
	appliedAt := `SELECT string_agg(version||'@'||applied_at::text, ',' ORDER BY version) FROM schema_migrations`
	before := d.text(t, appliedAt)

	var logs lockedBuffer
	if err := migrator("../../migrations", &logs).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if d.number(t, newColumns) != 2 || d.text(t, appliedAt) != before {
		t.Fatal("metadata not expanded in place")
	}
	if d.number(t, "SELECT stock FROM products WHERE id='p01'") != 23 || d.number(t, "SELECT count(*) FROM products") != 24 {
		t.Fatal("migration SQL ran again")
	}
	d.assertChecksumsMatchRepository(t)
	if strings.Count(logs.String(), "msg=\"migration checksum adopted\"") != 7 || strings.Contains(logs.String(), "migration applied") || !strings.Contains(logs.String(), "migration metadata bootstrapped") {
		t.Fatalf("logs: %s", logs.String())
	}
	if err := m.Verify(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	var again lockedBuffer
	if err := migrator("../../migrations", &again).Migrate(d.ctx, d.pool); err != nil || strings.Contains(again.String(), "msg=\"migration checksum adopted\"") || strings.Contains(again.String(), "msg=\"migration metadata bootstrapped\"") || !strings.Contains(again.String(), "adopted=0") {
		t.Fatalf("second run: %v %s", err, again.String())
	}

	// A recorded checksum is never re-adopted or overwritten.
	d.exec(t, "UPDATE schema_migrations SET checksum=repeat('0',64) WHERE version='001_init'")
	if err := m.Migrate(d.ctx, d.pool); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("mismatch: %v", err)
	}
	if err := m.Verify(d.ctx, d.pool); err == nil || errors.Is(err, postgres.ErrSchemaNotCurrent) {
		t.Fatalf("verify mismatch: %v", err)
	}
	if d.text(t, "SELECT checksum FROM schema_migrations WHERE version='001_init'") != strings.Repeat("0", 64) {
		t.Fatal("mismatched checksum overwritten")
	}
	// A NULL checksum is adopted only for the historical bytes.
	d.exec(t, "UPDATE schema_migrations SET checksum=NULL WHERE version='001_init'")
	edited := migrationDir(t, nil, nil)
	writeFile(t, edited, "001_init.up.sql", "-- edited after it was applied\n")
	if err := migrator(edited, &lockedBuffer{}).Migrate(d.ctx, d.pool); err == nil || !strings.Contains(err.Error(), "historical checksum") {
		t.Fatalf("adopted an edited historical file: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM schema_migrations WHERE version='001_init' AND checksum IS NULL") != 1 {
		t.Fatal("edited checksum recorded")
	}
	if !d.migrationLockFree(t) {
		t.Fatal("migration lock still held")
	}
}

func TestMigratorConcurrentRunsSerialize(t *testing.T) {
	d := newDatabase(t, false)
	// A second application would violate the primary key.
	dir := migrationDir(t, []string{}, map[string]string{
		"001_probe.up.sql":   "CREATE TABLE concurrent_probe(id int PRIMARY KEY); INSERT INTO concurrent_probe VALUES (1);",
		"002_probe.up.sql":   "INSERT INTO concurrent_probe VALUES (2);",
		"003_probe.up.sql":   "INSERT INTO concurrent_probe VALUES (3);",
		"003_probe.down.sql": "",
	})
	other, err := pgxpool.NewWithConfig(d.ctx, d.pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	release := d.holdMigrationLock(t)
	logs := []*lockedBuffer{{}, {}}
	errs := make(chan error, 2)
	for i, pool := range []*pgxpool.Pool{d.pool, other} {
		go func(m postgres.Migrator, pool *pgxpool.Pool) { errs <- m.Migrate(d.ctx, pool) }(migrator(dir, logs[i]), pool)
	}
	waitFor(t, "both migrators to wait", func() bool {
		return strings.Contains(logs[0].String(), "waiting for the migration lock") && strings.Contains(logs[1].String(), "waiting for the migration lock")
	})
	release()
	for range logs {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if d.text(t, "SELECT string_agg(id::text, ',' ORDER BY id) FROM concurrent_probe") != "1,2,3" || d.number(t, "SELECT count(*) FROM schema_migrations") != 3 {
		t.Fatal("migrations not applied exactly once")
	}
	applied := []int{strings.Count(logs[0].String(), "msg=\"migration applied\""), strings.Count(logs[1].String(), "msg=\"migration applied\"")}
	sort.Ints(applied)
	if !reflect.DeepEqual(applied, []int{0, 3}) {
		t.Fatalf("applied per migrator: %v", applied)
	}
	if !d.migrationLockFree(t) {
		t.Fatal("migration lock still held")
	}
}

func TestMigratorRejectsKnownGap(t *testing.T) {
	d := newDatabase(t, false)
	files := map[string]string{
		"001_base.up.sql":  "CREATE TABLE gap_base(id int);",
		"003_later.up.sql": "CREATE TABLE gap_later(id int);",
	}
	dir := migrationDir(t, []string{}, files)
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "002_missing.up.sql", "CREATE TABLE gap_missing(id int);")
	m := migrator(dir, &lockedBuffer{})
	if err := m.Migrate(d.ctx, d.pool); err == nil || !strings.Contains(err.Error(), "is pending but later migration 003_later is already applied") {
		t.Fatalf("gap: %v", err)
	}
	if err := m.Verify(d.ctx, d.pool); err == nil || errors.Is(err, postgres.ErrSchemaNotCurrent) {
		t.Fatalf("verify gap: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM pg_class WHERE relname='gap_missing'") != 0 {
		t.Fatal("gap migration executed")
	}
}

func TestMigratorToleratesNewerDatabaseVersion(t *testing.T) {
	d := newDatabase(t, false)
	dir := migrationDir(t, []string{}, map[string]string{"001_base.up.sql": "CREATE TABLE newer_base(id int);"})
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	// Written by a newer binary before this image was rolled back to.
	d.exec(t, "INSERT INTO schema_migrations(version,checksum) VALUES ('005_future',repeat('e',64))")
	var logs lockedBuffer
	m := migrator(dir, &logs)
	if err := m.Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "newer than this binary") || !strings.Contains(logs.String(), "005_future") {
		t.Fatalf("no warning: %s", logs.String())
	}
	if status, err := m.Status(d.ctx, d.pool); err != nil || !status.Current() || !reflect.DeepEqual(status.Newer, []string{"005_future"}) {
		t.Fatalf("status: %+v %v", status, err)
	}
	// An unknown version inside the known range is not a rollback artifact.
	d.exec(t, "INSERT INTO schema_migrations(version,checksum) VALUES ('000_manual',repeat('e',64))")
	if err := m.Migrate(d.ctx, d.pool); err == nil || !strings.Contains(err.Error(), "000_manual") {
		t.Fatalf("unknown lower version: %v", err)
	}
	if err := m.Verify(d.ctx, d.pool); err == nil {
		t.Fatal("verify accepted unknown lower version")
	}
}

// schemaFingerprint describes every business object except migration metadata.
const schemaFingerprint = `SELECT string_agg(item, E'\n' ORDER BY item) FROM (
	SELECT format('column %s.%s %s %s %s', table_name, column_name, data_type, is_nullable, coalesce(column_default, '')) AS item
		FROM information_schema.columns WHERE table_schema='public' AND table_name<>'schema_migrations'
	UNION ALL SELECT format('index %s', indexdef) FROM pg_indexes WHERE schemaname='public' AND tablename<>'schema_migrations'
	UNION ALL SELECT format('constraint %s %s', conname, pg_get_constraintdef(oid)) FROM pg_constraint
		WHERE connamespace='public'::regnamespace AND conrelid<>'schema_migrations'::regclass
	UNION ALL SELECT format('sequence %s', sequence_name) FROM information_schema.sequences WHERE sequence_schema='public'
	UNION ALL SELECT format('extension %s', extname) FROM pg_extension) objects`

func TestMigratorMetadataStaysCompatibleWithPreviousBinary(t *testing.T) {
	legacy := newDatabase(t, false)
	if err := legacyMigrate(legacy.ctx, legacy.pool, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	d := newDatabase(t, true)
	// A1 changes no business schema: the same objects as the previous runner.
	if d.text(t, schemaFingerprint) != legacy.text(t, schemaFingerprint) {
		t.Fatal("business schema differs from the previous binary's")
	}
	// The previous runner (RUN_MIGRATIONS=true) finds nothing to do.
	if err := legacyMigrate(d.ctx, d.pool, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	d.assertChecksumsMatchRepository(t)
	// The previous binary's INSERT shape still satisfies the expanded table.
	d.exec(t, "INSERT INTO schema_migrations(version) VALUES ('900_previous_binary_shape')")
	if d.number(t, "SELECT count(*) FROM schema_migrations WHERE version='900_previous_binary_shape' AND checksum IS NULL AND outcome='applied'") != 1 {
		t.Fatal("previous insert shape")
	}
	if err := migrator("../../migrations", &lockedBuffer{}).Verify(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
}

func TestMigratorLockTimeoutFailsCleanly(t *testing.T) {
	d := newDatabase(t, false)
	dir := migrationDir(t, []string{}, map[string]string{"001_blocked.up.sql": "CREATE TABLE blocked_probe(id int);"})
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "002_alter.up.sql", "ALTER TABLE blocked_probe ADD COLUMN extra int;")
	blocker, err := d.pool.Begin(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(d.ctx)
	if _, err := blocker.Exec(d.ctx, "LOCK TABLE blocked_probe IN ACCESS SHARE MODE"); err != nil {
		t.Fatal(err)
	}
	m := migrator(dir, &lockedBuffer{})
	m.LockTimeout = 200 * time.Millisecond
	started := time.Now()
	err = m.Migrate(d.ctx, d.pool)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("lock timeout: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("lock wait was not bounded: %v", elapsed)
	}
	if d.number(t, "SELECT count(*) FROM schema_migrations WHERE version='002_alter'") != 0 || d.number(t, "SELECT count(*) FROM pg_attribute WHERE attrelid='blocked_probe'::regclass AND attname='extra'") != 0 {
		t.Fatal("blocked migration recorded or partially applied")
	}
	if !d.migrationLockFree(t) {
		t.Fatal("migration lock still held")
	}
	if err := blocker.Rollback(d.ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
}

func TestMigratorReleasesLockOnFailureAndCancellation(t *testing.T) {
	d := newDatabase(t, false)
	dir := migrationDir(t, []string{}, map[string]string{"001_fails.up.sql": "CREATE TABLE fails_probe(id int); SELECT deliberately_missing_function();"})
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err == nil {
		t.Fatal("failing migration succeeded")
	}
	if !d.migrationLockFree(t) {
		t.Fatal("lock held after failure")
	}
	if d.number(t, "SELECT count(*) FROM schema_migrations") != 0 || d.number(t, "SELECT count(*) FROM pg_class WHERE relname='fails_probe'") != 0 {
		t.Fatal("failed migration recorded or partially applied")
	}

	writeFile(t, dir, "001_fails.up.sql", "CREATE TABLE fails_probe(id int);")
	release := d.holdMigrationLock(t)
	ctx, cancel := context.WithTimeout(d.ctx, 300*time.Millisecond)
	defer cancel()
	if err := migrator(dir, &lockedBuffer{}).Migrate(ctx, d.pool); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled wait: %v", err)
	}
	release()
	if !d.migrationLockFree(t) {
		t.Fatal("lock held after cancellation")
	}
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
}

func TestSimpleProtocolCommitEscapesTheTransaction(t *testing.T) {
	// Root cause: inside one simple query, a top-level COMMIT commits the
	// statements before it even though the string later fails.
	d := newDatabase(t, false)
	conn, err := d.pool.Acquire(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	_, err = conn.Conn().PgConn().Exec(d.ctx, "BEGIN; CREATE TABLE escaped_probe(id int); COMMIT; SELECT deliberately_missing_function();").ReadAll()
	if err == nil || d.number(t, "SELECT count(*) FROM pg_class WHERE relname='escaped_probe'") != 1 {
		t.Fatalf("expected the pre-COMMIT DDL to persist: %v", err)
	}
}

func TestMigratorRejectsTransactionControlBeforeExecution(t *testing.T) {
	d := newDatabase(t, false)
	dir := migrationDir(t, []string{}, map[string]string{"001_base.up.sql": "CREATE TABLE tc_base(id int);"})
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"COMMIT;", "commit work;", "BEGIN;", "START TRANSACTION;", "END;", "ROLLBACK;", "ABORT;", "SAVEPOINT s;", "RELEASE SAVEPOINT s;", "ROLLBACK TO SAVEPOINT s;", "PREPARE TRANSACTION 'tc';", "COMMIT PREPARED 'tc';"} {
		t.Run(control, func(t *testing.T) {
			writeFile(t, dir, "002_control.up.sql", "CREATE TABLE tc_probe(id int);\n-- the next statement is forbidden\n"+control+"\nCREATE TABLE tc_after(id int);\n")
			m := migrator(dir, &lockedBuffer{})
			err := m.Migrate(d.ctx, d.pool)
			if err == nil || !strings.Contains(err.Error(), "migration 002_control: line 3:") || !strings.Contains(err.Error(), "is transaction control") {
				t.Fatalf("migrate: %v", err)
			}
			if _, err := m.Status(d.ctx, d.pool); err == nil {
				t.Fatal("status accepted transaction control")
			}
			if err := m.Verify(d.ctx, d.pool); err == nil || errors.Is(err, postgres.ErrSchemaNotCurrent) {
				t.Fatalf("verify: %v", err)
			}
			if d.number(t, "SELECT count(*) FROM pg_class WHERE relname IN ('tc_probe','tc_after')") != 0 {
				t.Fatal("statements of the rejected file executed")
			}
			if d.text(t, "SELECT string_agg(version, ',' ORDER BY version) FROM schema_migrations") != "001_base" || d.number(t, "SELECT count(*) FROM pg_class WHERE relname='tc_base'") != 1 {
				t.Fatal("rejected migration recorded or earlier migration lost")
			}
			if !d.migrationLockFree(t) {
				t.Fatal("migration lock held")
			}
		})
	}

	// The whole set is checked before the lock: an invalid file blocks the
	// valid pending files of the same run too.
	fresh := newDatabase(t, false)
	set := migrationDir(t, []string{}, map[string]string{
		"001_first.up.sql":  "CREATE TABLE tc_first(id int);",
		"002_second.up.sql": "CREATE TABLE tc_second(id int);\nCOMMIT;",
	})
	if err := migrator(set, &lockedBuffer{}).Migrate(fresh.ctx, fresh.pool); err == nil {
		t.Fatal("accepted")
	}
	if fresh.number(t, "SELECT count(*) FROM pg_class WHERE relname IN ('tc_first','tc_second','schema_migrations')") != 0 {
		t.Fatal("a run with an invalid file executed SQL")
	}
}

func TestMigratorAcceptsTransactionWordsOutsideStatements(t *testing.T) {
	d := newDatabase(t, false)
	body := `-- COMMIT; BEGIN; ROLLBACK;
/* END; /* nested COMMIT; */ ABORT; */
CREATE TABLE commit_log(id int, begin_at text, "commit" text, "END" text, rollback_reason text);
INSERT INTO commit_log VALUES (1, 'BEGIN; COMMIT;', E'it\'s; ROLLBACK;', U&'END;', 'it''s; SAVEPOINT s;');
CREATE FUNCTION tw_plpgsql() RETURNS text LANGUAGE plpgsql AS $$
BEGIN
  -- COMMIT; inside a body is not a top-level statement
  RETURN 'commit; end;';
END;
$$;
CREATE FUNCTION tw_tagged() RETURNS text LANGUAGE plpgsql AS $body$ BEGIN RETURN $$ROLLBACK;$$; END $body$;
CREATE FUNCTION tw_atomic(x int) RETURNS int LANGUAGE sql
BEGIN ATOMIC
  SELECT CASE WHEN x > 0 THEN 1 ELSE 0 END;
END;
CREATE TABLE tw_a(id int); CREATE TABLE tw_b(id int); CREATE TABLE tw_t(id int);
CREATE RULE tw_rule AS ON INSERT TO tw_t DO ALSO (INSERT INTO tw_a VALUES (1); INSERT INTO tw_b VALUES (2));
DO $$ BEGIN PERFORM 1; END $$;
SELECT 1 AS begin, 2 AS commit;
`
	dir := migrationDir(t, []string{}, map[string]string{"001_words.up.sql": body})
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	d.exec(t, "INSERT INTO tw_t VALUES (9)")
	if d.text(t, `SELECT "commit"||'|'||rollback_reason||'|'||tw_plpgsql()||'|'||tw_tagged()||'|'||tw_atomic(5) FROM commit_log`) != "it's; ROLLBACK;|it's; SAVEPOINT s;|commit; end;|ROLLBACK;|1" ||
		d.number(t, "SELECT (SELECT count(*) FROM tw_a)+(SELECT count(*) FROM tw_b)") != 2 {
		t.Fatal("migration content not applied as written")
	}
}

func TestMigratorRequiresStandardConformingStrings(t *testing.T) {
	d := newDatabase(t, false)
	config := d.pool.Config()
	config.ConnConfig.RuntimeParams["standard_conforming_strings"] = "off"
	nonStandard, err := pgxpool.NewWithConfig(d.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nonStandard.Close)
	dir := migrationDir(t, []string{}, map[string]string{"001_probe.up.sql": "CREATE TABLE scs_probe(id int);"})
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, nonStandard); err == nil || !strings.Contains(err.Error(), "standard_conforming_strings must be on") {
		t.Fatalf("non-standard session: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM pg_class WHERE relname='scs_probe'") != 0 || d.number(t, "SELECT count(*) FROM schema_migrations") != 0 {
		t.Fatal("migration ran with non-standard strings")
	}
	// An earlier file of the same run cannot switch it off for a later one.
	writeFile(t, dir, "000_switch.up.sql", "SET standard_conforming_strings = off;")
	if err := migrator(dir, &lockedBuffer{}).Migrate(d.ctx, d.pool); err == nil || !strings.Contains(err.Error(), "001_probe: standard_conforming_strings must be on") {
		t.Fatalf("switched off mid-run: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM pg_class WHERE relname='scs_probe'") != 0 || d.text(t, "SELECT string_agg(version, ',') FROM schema_migrations") != "000_switch" {
		t.Fatal("later migration ran with non-standard strings")
	}
}

func TestVerifyRejectsPendingAndMismatchWithoutExecuting(t *testing.T) {
	d := newDatabase(t, true)
	pending := migrationDir(t, nil, map[string]string{"900_verify_probe.up.sql": "CREATE TABLE verify_probe(id int);"})
	if err := migrator(pending, &lockedBuffer{}).Verify(d.ctx, d.pool); !errors.Is(err, postgres.ErrSchemaNotCurrent) || !strings.Contains(err.Error(), "1 pending") {
		t.Fatalf("pending: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM pg_class WHERE relname='verify_probe'") != 0 || d.number(t, "SELECT count(*) FROM schema_migrations") != 7 {
		t.Fatal("verify executed migration SQL")
	}
	mismatch := migrationDir(t, nil, nil)
	writeFile(t, mismatch, "007_repeat_commits.up.sql", "-- edited\n")
	err := migrator(mismatch, &lockedBuffer{}).Verify(d.ctx, d.pool)
	if err == nil || errors.Is(err, postgres.ErrSchemaNotCurrent) || !strings.Contains(err.Error(), "007_repeat_commits checksum mismatch") {
		t.Fatalf("mismatch: %v", err)
	}
	if err := migrator("../../migrations", &lockedBuffer{}).Verify(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
}

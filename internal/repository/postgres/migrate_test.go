package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClassifyMigrationFile(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		fails         bool
	}{
		{"001_init.up.sql", "001_init", false},
		{"004_brand_manufacturer_integrity.up.sql", "004_brand_manufacturer_integrity", false},
		{"20_second.up.sql", "20_second", false},
		{"001_init.down.sql", "", false},
		{"README.md", "", false},
		{".gitkeep", "", false},
		{"001_Init.up.sql", "", true},
		{"init.up.sql", "", true},
		{"001_init.sql", "", true},
		{"001-init.up.sql", "", true},
		{"001_init_.up.sql", "", true},
		{"001__init.up.sql", "", true},
		{"001_init.up.SQL", "", false}, // not ".sql": ignored like any non-SQL file
		{"99999999999999999999_overflow.up.sql", "", true},
	} {
		version, err := classifyMigrationFile(tc.name)
		if (err != nil) != tc.fails || version != tc.version {
			t.Errorf("%s: version=%q err=%v", tc.name, version, err)
		}
	}
}

func writeMigrationFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDiscoverMigrationsOrdersNumericallyAndHashesBytes(t *testing.T) {
	dir := writeMigrationFiles(t, map[string]string{
		"10_ten.up.sql":    "SELECT 10;\r\n",
		"9_nine.up.sql":    "SELECT 9;",
		"9_nine.down.sql":  "broken",
		"notes.txt":        "ignored",
		"002_two.up.sql":   "SELECT 2;",
		"002_two.down.sql": "",
	})
	if err := os.Mkdir(filepath.Join(dir, "100_directory.up.sql"), 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := discoverMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	versions := []string{}
	for _, file := range files {
		versions = append(versions, file.version)
	}
	if !reflect.DeepEqual(versions, []string{"002_two", "9_nine", "10_ten"}) {
		t.Fatal(versions)
	}
	sum := sha256.Sum256([]byte("SELECT 10;\r\n"))
	if files[2].checksum != hex.EncodeToString(sum[:]) {
		t.Fatal("checksum must cover the raw bytes, including CRLF")
	}
}

func TestDiscoverMigrationsRejectsAmbiguousDirectories(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"duplicate number":       {"001_a.up.sql": "", "001_b.up.sql": ""},
		"duplicate after zeroes": {"1_a.up.sql": "", "001_b.up.sql": ""},
		"malformed SQL name":     {"001_a.up.sql": "", "002 b.up.sql": ""},
		"empty":                  {"README.md": ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := discoverMigrations(writeMigrationFiles(t, files)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	if _, err := discoverMigrations(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing directory accepted")
	}
}

func TestHistoricalChecksumsPinRepositoryMigrations(t *testing.T) {
	files, err := discoverMigrations("../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, file := range files {
		if want, ok := historicalChecksums[file.version]; ok {
			seen++
			if file.checksum != want {
				t.Errorf("%s changed: historical migrations must stay byte-identical", file.version)
			}
		}
	}
	if seen != len(historicalChecksums) {
		t.Fatalf("found %d of %d historical migrations", seen, len(historicalChecksums))
	}
}

func file(number uint64, version, checksum string) migrationFile {
	return migrationFile{number: number, version: version, checksum: checksum}
}

func states(status SchemaStatus) string {
	parts := []string{}
	for _, entry := range status.Migrations {
		parts = append(parts, entry.Version+"="+string(entry.State))
	}
	return strings.Join(parts, ",")
}

func TestPlanMigrations(t *testing.T) {
	sum := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	files := []migrationFile{file(1, "001_a", sum), file(2, "002_b", sum), file(3, "003_c", sum)}
	applied := func(versions ...string) []appliedMigration {
		rows := []appliedMigration{}
		for _, version := range versions {
			rows = append(rows, appliedMigration{version: version, checksum: sum, outcome: "applied"})
		}
		return rows
	}

	plan, err := planMigrations(files, nil, FixtureApply)
	if err != nil || len(plan.pending) != 3 || states(plan.status) != "001_a=pending,002_b=pending,003_c=pending" {
		t.Fatalf("fresh: %v %s", err, states(plan.status))
	}
	plan, err = planMigrations(files, applied("001_a", "002_b"), FixtureApply)
	if err != nil || len(plan.pending) != 1 || plan.pending[0].version != "003_c" || plan.status.Current() {
		t.Fatalf("ordered pending: %v", err)
	}
	plan, err = planMigrations(files, applied("001_a", "002_b", "003_c"), FixtureApply)
	if err != nil || len(plan.pending) != 0 || !plan.status.Current() {
		t.Fatalf("current: %v", err)
	}

	plan, err = planMigrations(files, applied("001_a", "002_b", "003_c", "010_future", "004_next"), FixtureApply)
	if err != nil || !reflect.DeepEqual(plan.status.Newer, []string{"004_next", "010_future"}) || !plan.status.Current() {
		t.Fatalf("newer versions must be tolerated in order: %v %v", err, plan.status.Newer)
	}

	for name, rows := range map[string][]appliedMigration{
		"gap below applied":       applied("001_a", "003_c"),
		"gap below newer":         applied("001_a", "002_b", "010_future"),
		"unknown inside range":    {{version: "001_a", checksum: sum, outcome: "applied"}, {version: "002_renamed", checksum: sum, outcome: "applied"}},
		"unknown below first":     applied("000_older", "001_a"),
		"checksum mismatch":       {{version: "001_a", checksum: other, outcome: "applied"}},
		"unsupported outcome":     {{version: "001_a", checksum: sum, outcome: "skipped"}},
		"unrecognized version":    {{version: "manual fix", checksum: sum, outcome: "applied"}},
		"null non-historical sum": {{version: "001_a", outcome: "applied"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := planMigrations(files, rows, FixtureApply); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestPlanMigrationsAdoptsOnlyVerifiedHistoricalChecksums(t *testing.T) {
	historical := historicalChecksums["001_init"]
	files := []migrationFile{file(1, "001_init", historical), file(8, "008_new", strings.Repeat("c", 64))}
	plan, err := planMigrations(files, []appliedMigration{{version: "001_init", outcome: "applied"}}, FixtureApply)
	if err != nil || len(plan.adopt) != 1 || plan.adopt[0].version != "001_init" || len(plan.pending) != 1 {
		t.Fatalf("adoption: %v", err)
	}
	if states(plan.status) != "001_init=checksum-unrecorded,008_new=pending" || plan.status.Current() {
		t.Fatal(states(plan.status))
	}
	edited := []migrationFile{file(1, "001_init", strings.Repeat("d", 64))}
	if _, err := planMigrations(edited, []appliedMigration{{version: "001_init", outcome: "applied"}}, FixtureApply); err == nil {
		t.Fatal("adopted a file that differs from the historical migration")
	}
	if _, err := planMigrations(edited, []appliedMigration{{version: "001_init", checksum: historical, outcome: "applied"}}, FixtureApply); err == nil {
		t.Fatal("recorded checksum not enforced")
	}
}

func TestSchemaStatusRequiresCurrentMetadata(t *testing.T) {
	status := SchemaStatus{MetadataCurrent: false, Migrations: []MigrationEntry{{Version: "001_a", State: MigrationApplied}}}
	if status.Current() || !strings.Contains(status.summary(), "missing or outdated") {
		t.Fatal("legacy metadata reported current")
	}
	status.MetadataCurrent = true
	if !status.Current() || status.summary() != "" {
		t.Fatal("current schema")
	}
}

func TestFixturePolicyForAppMode(t *testing.T) {
	for mode, want := range map[string]FixturePolicy{"production": FixtureSkip, "development": FixtureApply, "demo": FixtureApply} {
		if got, err := FixturePolicyForAppMode(mode); err != nil || got != want {
			t.Errorf("%s: %v %v", mode, got, err)
		}
	}
	for _, mode := range []string{"", "Production", "staging", "test", " demo"} {
		if _, err := FixturePolicyForAppMode(mode); err == nil {
			t.Errorf("%q accepted", mode)
		}
	}
	if FixtureApply.String() != "apply" || FixtureSkip.String() != "skip" || FixturePolicy(0).String() != "invalid" {
		t.Fatal("policy names")
	}
}

func TestFixtureMigrationsAreExplicitHistoricalVersions(t *testing.T) {
	if len(fixtureMigrations) != 1 || !fixtureMigrations["002_demo_seed"] {
		t.Fatal(fixtureMigrations)
	}
	for version := range fixtureMigrations {
		if _, ok := historicalChecksums[version]; !ok {
			t.Errorf("fixture %s has no pinned checksum", version)
		}
	}
	// A2 identifies fixtures by exact version, never by number or name shape.
	for _, version := range []string{"002_probe", "002_demo", "2_demo_seed", "020_demo_seed"} {
		if fixtureMigrations[version] {
			t.Errorf("%s treated as fixture", version)
		}
	}
}

func TestMigratorRequiresAFixturePolicy(t *testing.T) {
	for _, policy := range []FixturePolicy{0, 3, -1} {
		if err := (Migrator{Directory: "../../../migrations", Fixtures: policy}).validate(); err == nil {
			t.Errorf("policy %d accepted", policy)
		}
	}
}

// fixtureFiles is the real fixture between two ordinary migrations.
func fixtureFiles() []migrationFile {
	sum := strings.Repeat("a", 64)
	return []migrationFile{file(1, "001_init", sum), file(2, "002_demo_seed", historicalChecksums["002_demo_seed"]), file(3, "003_next", sum)}
}

func row(version, checksum, outcome string) appliedMigration {
	return appliedMigration{version: version, checksum: checksum, outcome: outcome}
}

func TestPlanMigrationsFixturePolicy(t *testing.T) {
	files := fixtureFiles()
	sum, fixture := files[0].checksum, files[1].checksum

	plan, err := planMigrations(files, nil, FixtureSkip)
	if err != nil || states(plan.status) != "001_init=pending,002_demo_seed=pending-skip,003_next=pending" {
		t.Fatalf("fresh skip: %v %s", err, states(plan.status))
	}
	if len(plan.pending) != 3 || plan.pending[0].skip || !plan.pending[1].skip || plan.pending[2].skip || !plan.status.Migrations[1].Fixture || plan.status.Migrations[0].Fixture {
		t.Fatalf("skip plan: %+v", plan.pending)
	}
	plan, err = planMigrations(files, nil, FixtureApply)
	if err != nil || states(plan.status) != "001_init=pending,002_demo_seed=pending,003_next=pending" || plan.pending[1].skip {
		t.Fatalf("fresh apply: %v %s", err, states(plan.status))
	}

	skippedDB := []appliedMigration{row("001_init", sum, "applied"), row("002_demo_seed", fixture, "skipped"), row("003_next", sum, "applied")}
	appliedDB := []appliedMigration{row("001_init", sum, "applied"), row("002_demo_seed", fixture, "applied"), row("003_next", sum, "applied")}

	plan, err = planMigrations(files, skippedDB, FixtureSkip)
	if err != nil || !plan.status.Current() || states(plan.status) != "001_init=applied,002_demo_seed=skipped,003_next=applied" {
		t.Fatalf("skip on skipped: %v %s", err, states(plan.status))
	}
	if plan, err = planMigrations(files, appliedDB, FixtureApply); err != nil || !plan.status.Current() {
		t.Fatalf("apply on applied: %v", err)
	}
	_, err = planMigrations(files, appliedDB, FixtureSkip)
	if !errors.Is(err, ErrFixturePolicyConflict) || !errors.Is(err, ErrDemoFixturesApplied) || !strings.Contains(err.Error(), "fresh production database") || strings.Contains(err.Error(), "shopctl migrate") {
		t.Fatalf("skip on applied: %v", err)
	}
	_, err = planMigrations(files, skippedDB, FixtureApply)
	if !errors.Is(err, ErrFixturePolicyConflict) || errors.Is(err, ErrDemoFixturesApplied) {
		t.Fatalf("apply on skipped: %v", err)
	}
	// A pre-A1 database with an unrecorded fixture checksum is still applied demo data.
	if _, err := planMigrations(files, []appliedMigration{row("001_init", sum, "applied"), row("002_demo_seed", "", "applied")}, FixtureSkip); !errors.Is(err, ErrDemoFixturesApplied) {
		t.Fatalf("unrecorded applied fixture under skip: %v", err)
	}
}

func TestPlanMigrationsFixtureInvariants(t *testing.T) {
	files := fixtureFiles()
	sum, fixture := files[0].checksum, files[1].checksum
	for name, tc := range map[string]struct {
		files []migrationFile
		rows  []appliedMigration
	}{
		"missing fixture below a later version": {files, []appliedMigration{row("001_init", sum, "applied"), row("003_next", sum, "applied")}},
		"skipped fixture checksum mismatch":     {files, []appliedMigration{row("001_init", sum, "applied"), row("002_demo_seed", strings.Repeat("f", 64), "skipped")}},
		"skipped fixture without checksum":      {files, []appliedMigration{row("002_demo_seed", "", "skipped")}},
		"skipped non-fixture":                   {files, []appliedMigration{row("001_init", sum, "skipped")}},
		"unknown outcome":                       {files, []appliedMigration{row("001_init", sum, "purged")}},
		"edited fixture file, fresh database":   {[]migrationFile{files[0], file(2, "002_demo_seed", strings.Repeat("e", 64))}, nil},
		"edited fixture file, skipped database": {[]migrationFile{files[0], file(2, "002_demo_seed", strings.Repeat("e", 64))}, []appliedMigration{row("002_demo_seed", fixture, "skipped")}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, policy := range []FixturePolicy{FixtureApply, FixtureSkip} {
				if _, err := planMigrations(tc.files, tc.rows, policy); err == nil {
					t.Fatalf("accepted under %s", policy)
				}
			}
		})
	}
	// A newer binary may have skipped a future fixture; an older binary tolerates it.
	rows := []appliedMigration{row("001_init", sum, "applied"), row("002_demo_seed", fixture, "skipped"), row("003_next", sum, "applied"), row("009_future_fixture", sum, "skipped")}
	if plan, err := planMigrations(files, rows, FixtureSkip); err != nil || !reflect.DeepEqual(plan.status.Newer, []string{"009_future_fixture"}) {
		t.Fatalf("newer skipped version: %v", err)
	}
}

func TestSchemaStatusTreatsSkippedAsDone(t *testing.T) {
	status := SchemaStatus{MetadataCurrent: true, Migrations: []MigrationEntry{{State: MigrationApplied}, {State: MigrationSkipped, Fixture: true}}}
	if !status.Current() {
		t.Fatal("skipped fixture is complete")
	}
	status.Migrations[1].State = MigrationPendingSkip
	if status.Current() || status.summary() != "1 pending" {
		t.Fatalf("pending skip: %q", status.summary())
	}
}

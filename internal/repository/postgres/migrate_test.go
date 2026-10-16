package postgres

import (
	"crypto/sha256"
	"encoding/hex"
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

	plan, err := planMigrations(files, nil)
	if err != nil || len(plan.pending) != 3 || states(plan.status) != "001_a=pending,002_b=pending,003_c=pending" {
		t.Fatalf("fresh: %v %s", err, states(plan.status))
	}
	plan, err = planMigrations(files, applied("001_a", "002_b"))
	if err != nil || len(plan.pending) != 1 || plan.pending[0].version != "003_c" || plan.status.Current() {
		t.Fatalf("ordered pending: %v", err)
	}
	plan, err = planMigrations(files, applied("001_a", "002_b", "003_c"))
	if err != nil || len(plan.pending) != 0 || !plan.status.Current() {
		t.Fatalf("current: %v", err)
	}

	plan, err = planMigrations(files, applied("001_a", "002_b", "003_c", "010_future", "004_next"))
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
			if _, err := planMigrations(files, rows); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestPlanMigrationsAdoptsOnlyVerifiedHistoricalChecksums(t *testing.T) {
	historical := historicalChecksums["001_init"]
	files := []migrationFile{file(1, "001_init", historical), file(8, "008_new", strings.Repeat("c", 64))}
	plan, err := planMigrations(files, []appliedMigration{{version: "001_init", outcome: "applied"}})
	if err != nil || len(plan.adopt) != 1 || plan.adopt[0].version != "001_init" || len(plan.pending) != 1 {
		t.Fatalf("adoption: %v", err)
	}
	if states(plan.status) != "001_init=checksum-unrecorded,008_new=pending" || plan.status.Current() {
		t.Fatal(states(plan.status))
	}
	edited := []migrationFile{file(1, "001_init", strings.Repeat("d", 64))}
	if _, err := planMigrations(edited, []appliedMigration{{version: "001_init", outcome: "applied"}}); err == nil {
		t.Fatal("adopted a file that differs from the historical migration")
	}
	if _, err := planMigrations(edited, []appliedMigration{{version: "001_init", checksum: historical, outcome: "applied"}}); err == nil {
		t.Fatal("recorded checksum not enforced")
	}
}

func TestSchemaStatusRequiresCurrentMetadata(t *testing.T) {
	status := SchemaStatus{MetadataCurrent: false, Migrations: []MigrationEntry{{Version: "001_a", State: MigrationApplied}}}
	if status.Current() || !strings.Contains(status.summary(), "predates checksums") {
		t.Fatal("legacy metadata reported current")
	}
	status.MetadataCurrent = true
	if !status.Current() || status.summary() != "" {
		t.Fatal("current schema")
	}
}

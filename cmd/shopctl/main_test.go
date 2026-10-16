package main

import (
	"bytes"
	"strings"
	"testing"

	postgresrepo "sweetshop/internal/repository/postgres"
)

func TestUsageAndConfigurationExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		env  map[string]string
		want int
	}{
		{"no command", nil, nil, exitUsage},
		{"unknown command", []string{"seed"}, nil, exitUsage},
		{"help", []string{"help"}, nil, exitOK},
		{"migrate help", []string{"migrate", "-h"}, nil, exitOK},
		{"unknown flag", []string{"migrate", "--force"}, nil, exitUsage},
		{"extra argument", []string{"migrate", "now"}, nil, exitUsage},
		{"zero timeout", []string{"migrate", "--timeout=0"}, map[string]string{"DATABASE_URL": "postgres://localhost/db"}, exitUsage},
		{"negative lock timeout", []string{"migrate", "--lock-timeout=-1s"}, map[string]string{"DATABASE_URL": "postgres://localhost/db"}, exitUsage},
		{"bad mode", []string{"migrate"}, map[string]string{"APP_MODE": "staging", "DATABASE_URL": "postgres://localhost/db"}, exitUsage},
		{"missing DSN", []string{"migrate", "--status"}, map[string]string{"DATABASE_URL": " "}, exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_MODE", "")
			t.Setenv("DATABASE_URL", "")
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, &stdout, &stderr); got != tc.want {
				t.Fatalf("exit=%d want=%d stderr=%s", got, tc.want, stderr.String())
			}
		})
	}
}

func TestDatabaseCredentialsNeverPrinted(t *testing.T) {
	for name, tc := range map[string]struct {
		dsn  string
		args []string
		want int
	}{
		"invalid DSN": {"postgres://example:synthetic-secret@localhost/%zz", []string{"migrate"}, exitUsage},
		// Port 1 refuses connections: a runtime failure, not a usage error.
		"unreachable migrate": {"postgres://example:synthetic-secret@127.0.0.1:1/shop?connect_timeout=2", []string{"migrate", "--timeout=10s"}, exitFailure},
		"unreachable status":  {"postgres://example:synthetic-secret@127.0.0.1:1/shop?connect_timeout=2", []string{"migrate", "--status", "--timeout=10s"}, exitFailure},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("APP_MODE", "")
			t.Setenv("DATABASE_URL", tc.dsn)
			t.Setenv("MIGRATIONS_DIR", "../../migrations")
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, &stdout, &stderr); got != tc.want {
				t.Fatalf("exit=%d want=%d", got, tc.want)
			}
			if strings.Contains(stdout.String()+stderr.String(), "synthetic-secret") {
				t.Fatal("password printed")
			}
		})
	}
}

func TestPrintStatus(t *testing.T) {
	report := postgresrepo.SchemaStatus{
		MetadataCurrent: true,
		Migrations: []postgresrepo.MigrationEntry{
			{Version: "001_init", Checksum: strings.Repeat("a", 64), State: postgresrepo.MigrationApplied},
			{Version: "002_demo_seed", Checksum: strings.Repeat("c", 64), State: postgresrepo.MigrationSkipped, Fixture: true},
			{Version: "003_next", Checksum: strings.Repeat("b", 64), State: postgresrepo.MigrationPending},
		},
		Newer: []string{"009_future"},
	}
	var out bytes.Buffer
	printStatus(&out, postgresrepo.FixtureSkip, report)
	want := "metadata\tcurrent\nfixtures\tskip\n001_init\tapplied\taaaaaaaaaaaa\n002_demo_seed\tskipped\tcccccccccccc\tdemo-fixture\n003_next\tpending\tbbbbbbbbbbbb\n009_future\tnewer-than-binary\t-\nschema\tnot-current\n"
	if out.String() != want {
		t.Fatalf("%q", out.String())
	}
}

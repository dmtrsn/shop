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
			if got := run(tc.args, strings.NewReader(""), &stdout, &stderr); got != tc.want {
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
			if got := run(tc.args, strings.NewReader(""), &stdout, &stderr); got != tc.want {
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

func TestCustomerUsageErrorsNeedNoDatabase(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  int
	}{
		{"no subcommand", []string{"customer"}, "", exitUsage},
		{"unknown subcommand", []string{"customer", "delete", "--email", "a@b.co"}, "", exitUsage},
		{"help", []string{"customer", "help"}, "", exitOK},
		{"create without email", []string{"customer", "create", "--name", "A", "--password-stdin"}, "correct horse\n", exitUsage},
		{"create without password-stdin", []string{"customer", "create", "--email", "a@b.co", "--name", "A"}, "correct horse\n", exitUsage},
		{"password as flag", []string{"customer", "create", "--email", "a@b.co", "--name", "A", "--password", "correct horse"}, "", exitUsage},
		{"set-password without stdin flag", []string{"customer", "set-password", "--email", "a@b.co"}, "correct horse\n", exitUsage},
		{"empty stdin", []string{"customer", "set-password", "--email", "a@b.co", "--password-stdin"}, "", exitUsage},
		{"two lines", []string{"customer", "set-password", "--email", "a@b.co", "--password-stdin"}, "one\ntwo\n", exitUsage},
		{"disable takes no password flag", []string{"customer", "disable", "--email", "a@b.co", "--password-stdin"}, "", exitUsage},
		{"extra argument", []string{"customer", "enable", "--email", "a@b.co", "now"}, "", exitUsage},
		{"no DSN", []string{"customer", "disable", "--email", "a@b.co"}, "", exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_MODE", "")
			t.Setenv("DATABASE_URL", "")
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, strings.NewReader(tc.stdin), &stdout, &stderr); got != tc.want {
				t.Fatalf("exit=%d want=%d stderr=%s", got, tc.want, stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), "correct horse") {
				t.Fatal("password echoed")
			}
		})
	}
}

func TestReadPassword(t *testing.T) {
	for input, want := range map[string]string{"correct horse\n": "correct horse", "correct horse\r\n": "correct horse", "no newline": "no newline", "  spaced  \n": "  spaced  "} {
		if got, err := readPassword(strings.NewReader(input)); err != nil || got != want {
			t.Errorf("%q: %q %v", input, got, err)
		}
	}
	for _, input := range []string{"", "\n", "a\nb", "a\n\n", "a\rb\n", strings.Repeat("x", maxPasswordInput+1)} {
		if _, err := readPassword(strings.NewReader(input)); err == nil {
			t.Errorf("%q accepted", input)
		}
	}
}

func TestCustomerPasswordNeverPrintedWhenTheDatabaseFails(t *testing.T) {
	t.Setenv("APP_MODE", "production")
	t.Setenv("DATABASE_URL", "postgres://example:synthetic-secret@127.0.0.1:1/shop?connect_timeout=2")
	t.Setenv("MIGRATIONS_DIR", "../../migrations")
	var stdout, stderr bytes.Buffer
	code := run([]string{"customer", "create", "--email", "a@b.co", "--name", "A", "--password-stdin"}, strings.NewReader("correct horse battery\n"), &stdout, &stderr)
	if code != exitFailure || strings.Contains(stdout.String()+stderr.String(), "correct horse") || strings.Contains(stdout.String()+stderr.String(), "synthetic-secret") {
		t.Fatalf("exit=%d output=%s%s", code, stdout.String(), stderr.String())
	}
}

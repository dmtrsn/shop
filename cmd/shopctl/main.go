// Command shopctl is the operator tool. It applies and reports database
// migrations with the same engine the server uses, provisions customer and
// staff accounts, and tests the SMTP configuration:
//
//	shopctl migrate [--timeout=10m] [--lock-timeout=5s]
//	shopctl migrate --status
//	shopctl customer create|set-password|disable|enable ...
//	shopctl staff create|set-password|disable|enable ...
//	shopctl email test --to ADDRESS
//
// Configuration follows the server: DATABASE_URL (required), MIGRATIONS_DIR
// (default "migrations") and APP_MODE (production, development or demo).
// Logs go to stderr and results to stdout. The DSN, passwords and password
// hashes are never printed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	postgresrepo "sweetshop/internal/repository/postgres"
)

const (
	exitOK = 0
	// exitFailure: the database is unreachable, a migration failed, or the
	// schema is inconsistent (checksum mismatch, gap, unknown version).
	exitFailure = 1
	exitUsage   = 2
	// exitNotCurrent: --status found pending work for `shopctl migrate`.
	exitNotCurrent = 3
	// exitConflict: the account email is taken, or no account has it.
	exitConflict = 3
)

const usage = `Usage:
  shopctl migrate [--timeout=10m] [--lock-timeout=5s]   apply pending migrations
  shopctl migrate --status                              report state; changes nothing
  shopctl customer create --email E --name N [--phone P] --password-stdin
  shopctl customer set-password --email E --password-stdin
  shopctl customer disable --email E
  shopctl customer enable --email E
  shopctl staff create --email E --name DISPLAY_NAME --password-stdin
  shopctl staff set-password|disable|enable --email E [--password-stdin]
  shopctl email test --to ADDRESS [--timeout=30s]       send one SMTP test message

Environment: DATABASE_URL (required), MIGRATIONS_DIR (default migrations),
APP_MODE (production, development or demo; default production). APP_MODE
alone sets the demo-fixture policy: production records 002_demo_seed as
skipped without running it; development and demo apply it.

Exit codes: 0 success or current, 1 failure or inconsistent schema,
2 usage, configuration or validation error, 3 --status found pending work
or the account email is taken / unknown.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "migrate":
		return migrate(args[1:], stdout, stderr)
	case "customer":
		return customer(args[1:], stdin, stdout, stderr)
	case "staff":
		return staff(args[1:], stdin, stdout, stderr)
	case "email":
		return emailCommand(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "shopctl: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

func migrate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	status := flags.Bool("status", false, "report migration state without changing the database")
	timeout := flags.Duration("timeout", 10*time.Minute, "overall limit, including the wait for the migration lock")
	lockTimeout := flags.Duration("lock-timeout", postgresrepo.DefaultMigrationLockTimeout, "per-migration lock_timeout for table locks")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "shopctl migrate: unexpected argument %q\n", flags.Arg(0))
		return exitUsage
	}
	if *timeout <= 0 || *lockTimeout <= 0 {
		fmt.Fprintln(stderr, "shopctl migrate: --timeout and --lock-timeout must be positive")
		return exitUsage
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	env, err := loadEnvironment()
	if err != nil {
		logger.Error(err.Error())
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, env.config)
	if err != nil {
		logger.Error("connect postgres", "error", err)
		return exitFailure
	}
	defer pool.Close()
	migrator := env.migrator(logger)
	migrator.LockTimeout = *lockTimeout
	fixtures, mode, directory := env.fixtures, env.mode, env.directory

	if *status {
		report, err := migrator.Status(ctx, pool)
		if err != nil {
			logger.Error("migration status failed", "error", err)
			return exitFailure
		}
		printStatus(stdout, fixtures, report)
		if !report.Current() {
			return exitNotCurrent
		}
		return exitOK
	}
	logger.Info("applying migrations", "mode", mode, "fixtures", fixtures.String(), "directory", directory)
	if err := migrator.Migrate(ctx, pool); err != nil {
		logger.Error("migration failed", "error", err)
		return exitFailure
	}
	return exitOK
}

// environment is the configuration shared by every command.
type environment struct {
	mode      string
	fixtures  postgresrepo.FixturePolicy
	config    *pgxpool.Config
	directory string
}

// loadEnvironment reads APP_MODE (default production), DATABASE_URL and
// MIGRATIONS_DIR. Its errors are configuration errors and never contain the
// DSN. The fixture policy comes only from APP_MODE; no flag overrides it.
func loadEnvironment() (environment, error) {
	env := environment{mode: os.Getenv("APP_MODE"), directory: os.Getenv("MIGRATIONS_DIR")}
	if env.mode == "" {
		env.mode = "production"
	}
	if env.directory == "" {
		env.directory = "migrations"
	}
	var err error
	if env.fixtures, err = postgresrepo.FixturePolicyForAppMode(env.mode); err != nil {
		return environment{}, err
	}
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return environment{}, errors.New("DATABASE_URL is required")
	}
	if env.config, err = pgxpool.ParseConfig(dsn); err != nil {
		// pgx parse errors can include the DSN and its credentials.
		return environment{}, errors.New("invalid DATABASE_URL")
	}
	env.config.MaxConns = 2
	env.config.MinConns = 0
	return env, nil
}

func (e environment) migrator(logger *slog.Logger) postgresrepo.Migrator {
	return postgresrepo.Migrator{Directory: e.directory, Fixtures: e.fixtures, Logger: logger}
}

// printStatus writes one tab-separated line per migration plus a summary. A
// demo-fixture migration has a fourth column.
func printStatus(w io.Writer, fixtures postgresrepo.FixturePolicy, report postgresrepo.SchemaStatus) {
	metadata := "current"
	if !report.MetadataCurrent {
		metadata = "legacy-or-absent"
	}
	fmt.Fprintf(w, "metadata\t%s\n", metadata)
	fmt.Fprintf(w, "fixtures\t%s\n", fixtures)
	for _, migration := range report.Migrations {
		marker := ""
		if migration.Fixture {
			marker = "\tdemo-fixture"
		}
		fmt.Fprintf(w, "%s\t%s\t%s%s\n", migration.Version, migration.State, migration.Checksum[:12], marker)
	}
	for _, version := range report.Newer {
		fmt.Fprintf(w, "%s\tnewer-than-binary\t-\n", version)
	}
	state := "current"
	if !report.Current() {
		state = "not-current"
	}
	fmt.Fprintf(w, "schema\t%s\n", state)
}

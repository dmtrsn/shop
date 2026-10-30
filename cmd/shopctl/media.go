package main

import (
	"context"
	"encoding/json"
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

	"sweetshop/internal/application"
	"sweetshop/internal/media"
	postgresrepo "sweetshop/internal/repository/postgres"
)

const mediaUsage = `Usage:
  shopctl media verify [--grace=24h] [--apply]

Compares the managed media store (MEDIA_ROOT) with the media_assets rows and
prints a JSON report (ADR 019):
  missing        live assets whose file is gone (served as 404; needs an operator)
  orphans        files with no row older than --grace (an upload that never committed)
  youngOrphans   files with no row inside --grace (possibly an upload in progress)
  deleted        files of deleted assets (a delete whose unlink failed)
  staleTemp      interrupted writes older than --grace

Without --apply nothing changes. --apply removes deleted, orphans and
staleTemp; never a live asset's file, never a young file, and never a file
outside the store's own layout. --grace must be at least 1h with --apply.
Orphans are kept if the database has no media rows at all (a likely wrong
DATABASE_URL).

Exit codes: 0 consistent (after --apply: nothing left to fix), 1 failure,
2 usage or configuration error, 3 problems found.
`

const mediaTimeout = 10 * time.Minute

func mediaCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "verify" {
		if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
			_, _ = io.WriteString(stdout, mediaUsage)
			return exitOK
		}
		_, _ = io.WriteString(stderr, mediaUsage)
		return exitUsage
	}
	flags := flag.NewFlagSet("media verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	grace := flags.Duration("grace", 24*time.Hour, "age after which an unregistered file is an orphan")
	apply := flags.Bool("apply", false, "remove deleted assets' files, orphans and stale temporary files")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "shopctl media verify: unexpected argument %q\n", flags.Arg(0))
		return exitUsage
	}
	if *grace <= 0 || (*apply && *grace < media.MinGrace) {
		fmt.Fprintf(stderr, "shopctl media verify: --grace must be positive, and at least %s with --apply\n", media.MinGrace)
		return exitUsage
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	env, err := loadEnvironment()
	if err != nil {
		logger.Error(err.Error())
		return exitUsage
	}
	files, err := media.OpenFileStore(strings.TrimSpace(os.Getenv("MEDIA_ROOT")))
	if err != nil {
		logger.Error("invalid MEDIA_ROOT", "error", err)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, mediaTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, env.config)
	if err != nil {
		logger.Error("connect postgres", "error", err)
		return exitFailure
	}
	defer pool.Close()
	if err := env.migrator(logger).Verify(ctx, pool); err != nil {
		logger.Error("schema verification failed", "error", err)
		return exitFailure
	}
	library := application.NewMedia(postgresrepo.New(pool), files, logger)
	report, err := library.Reconcile(ctx, *grace, *apply)
	if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
		return exitFailure
	}
	if err != nil {
		logger.Error("media verification failed", "error", err)
		return exitFailure
	}
	remaining := report.Problems()
	if *apply {
		remaining = len(report.Missing) > 0 || report.Refused != ""
	}
	if remaining {
		return exitNotCurrent
	}
	return exitOK
}

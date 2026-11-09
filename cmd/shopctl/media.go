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
	"sweetshop/internal/domain"
	"sweetshop/internal/media"
	postgresrepo "sweetshop/internal/repository/postgres"
)

const mediaUsage = `Usage:
  shopctl media verify [--grace=24h] [--apply]
  shopctl media migrate-legacy [--assets-dir=web/assets] [--apply --staff-email=E] [--timeout=30m]

verify compares the managed media store (MEDIA_ROOT) with the media_assets rows and
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

migrate-legacy imports legacy /assets product images into the managed library
(ADR 021) and prints a JSON report. By default it is a dry run that changes
nothing and classifies every product, reports missing or invalid files, and
lists which /assets files products, historical orders and static pages still
use. --apply migrates product by product and needs --staff-email: the active
staff account recorded as the actor of the audit events. It deletes no legacy
file and never rewrites order snapshots. Reruns are safe.

Exit codes: 0 nothing left to do, 1 failure, 2 usage or configuration error,
3 products still need migration or attention.
`

const (
	mediaTimeout         = 10 * time.Minute
	legacyMigrateTimeout = 30 * time.Minute
)

func mediaCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "migrate-legacy" {
		return mediaMigrateLegacy(args[1:], stdout, stderr)
	}
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

func mediaMigrateLegacy(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("media migrate-legacy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	assetsDir := flags.String("assets-dir", "web/assets", "directory of the legacy /assets files")
	apply := flags.Bool("apply", false, "migrate products; without it nothing changes")
	staffEmail := flags.String("staff-email", "", "active staff account recorded as the actor (required with --apply)")
	timeout := flags.Duration("timeout", legacyMigrateTimeout, "overall limit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "shopctl media migrate-legacy: unexpected argument %q\n", flags.Arg(0))
		return exitUsage
	}
	if *timeout <= 0 || *apply != (strings.TrimSpace(*staffEmail) != "") {
		fmt.Fprintln(stderr, "shopctl media migrate-legacy: --timeout must be positive, and --apply and --staff-email go together")
		return exitUsage
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	env, err := loadEnvironment()
	if err != nil {
		logger.Error(err.Error())
		return exitUsage
	}
	if info, err := os.Stat(*assetsDir); err != nil || !info.IsDir() {
		logger.Error("--assets-dir is not a readable directory")
		return exitUsage
	}
	root := strings.TrimSpace(os.Getenv("MEDIA_ROOT"))
	openStore := media.OpenFileStoreReadOnly
	if *apply {
		openStore = media.OpenFileStore
	}
	files, err := openStore(root)
	if err != nil {
		logger.Error("invalid MEDIA_ROOT", "error", err)
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
	if err := env.migrator(logger).Verify(ctx, pool); err != nil {
		logger.Error("schema verification failed", "error", err)
		return exitFailure
	}
	store := postgresrepo.New(pool)
	var staff domain.StaffUser
	if *apply {
		credential, err := store.FindStaffByEmail(ctx, strings.TrimSpace(*staffEmail))
		if err != nil || credential.Disabled {
			logger.Error("--staff-email must name an active staff account")
			return exitUsage
		}
		staff = credential.Staff
	}
	requestID := "migrate-legacy:" + time.Now().UTC().Format("20060102T150405")
	report, err := application.NewLegacyMediaMigration(store, application.NewMedia(store, files, logger), *assetsDir, logger).
		Run(ctx, *apply, staff, requestID)
	if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
		return exitFailure
	}
	if err != nil {
		logger.Error("legacy media migration failed", "error", err)
		return exitFailure
	}
	if report.Pending() {
		return exitNotCurrent
	}
	return exitOK
}

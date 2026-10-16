package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationLockKey is the session-level advisory lock ("sweetmig" in ASCII)
// held by every migrator: shopctl migrate, development startup and tests.
// Advisory locks are scoped to one database, so it serializes migrators of
// that database only.
const MigrationLockKey int64 = 0x7377_6565_746d_6967

// DefaultMigrationLockTimeout bounds each migration statement's wait for a
// table lock, so DDL fails instead of queueing indefinitely behind checkout.
const DefaultMigrationLockTimeout = 5 * time.Second

const migrationLockPollInterval = 250 * time.Millisecond

// ErrSchemaNotCurrent reports a consistent schema that `shopctl migrate` can
// bring up to date: pending migrations, unrecorded checksums or metadata that
// predates checksums. Inconsistent states (checksum mismatch, gaps, unknown
// versions) are other errors that migrate refuses as well.
var ErrSchemaNotCurrent = errors.New("database schema is not current")

// historicalChecksums are the SHA-256 digests of migrations that could have
// been applied before schema_migrations recorded checksums. Only these
// versions may have a NULL checksum, and it is adopted only if the file still
// has exactly these bytes. Every later migration records its checksum when it
// is applied. These files must never change.
var historicalChecksums = map[string]string{
	"001_init":                         "6f566d7ef61fb06d598e7f972304739fe913b42c87afd5818a8282e1b7477a6a",
	"002_demo_seed":                    "4c3a904ad84476fb794f7aa77d5618b2bde6210e0aef205446dedfd4a44e62d1",
	"003_catalog_scale_indexes":        "5488b564bd66aa2b05654e8bbae63eb5a621f6b0232b6db855a85198cffc943d",
	"004_brand_manufacturer_integrity": "3e99cfa4c3c5ce0bfa681da05d6406b3f329065fe825f2187c754847e5edb405",
	"005_session_transport":            "1e8da2091027358aa4f0e4971952ef681c63da03e80293ee0cf36fd1d5d258e7",
	"006_order_idempotency":            "50a0cde02d01539adcbd3e2c1fa84f51f29c149ff818b3a282cb46d0bb6f80f0",
	"007_repeat_commits":               "c51eb8d05f01d3652b49c52c540310f246a5698a80653d38c22b5bff8f1c96e5",
}

// Migrator applies and verifies the *.up.sql files of Directory.
//
// Migrate owns one connection taken out of the pool for its whole run and
// holds MigrationLockKey on it. The connection is closed, never returned to
// the pool, so the lock cannot outlive the run in a pooled session. Each
// migration and its schema_migrations row commit in one transaction.
//
// Metadata stays compatible with earlier binaries: the added checksum and
// outcome columns are nullable or defaulted, so their
// INSERT INTO schema_migrations(version) still succeeds.
type Migrator struct {
	Directory string
	// LockTimeout is SET LOCAL lock_timeout for each migration; zero means
	// DefaultMigrationLockTimeout.
	LockTimeout time.Duration
	// Logger receives progress and warnings; nil discards them.
	Logger *slog.Logger
}

// Migrate applies the pending migrations of directory. It is the convenience
// form used by development startup and tests.
func Migrate(ctx context.Context, pool *pgxpool.Pool, directory string) error {
	return Migrator{Directory: directory}.Migrate(ctx, pool)
}

// MigrationState is the state of one known migration in the database.
type MigrationState string

const (
	MigrationApplied MigrationState = "applied"
	MigrationPending MigrationState = "pending"
	// MigrationChecksumUnrecorded is applied by an earlier binary; the next
	// migrate records its verified historical checksum.
	MigrationChecksumUnrecorded MigrationState = "checksum-unrecorded"
)

type MigrationEntry struct {
	Version  string
	Checksum string
	State    MigrationState
}

// SchemaStatus describes a database relative to this binary's migrations.
type SchemaStatus struct {
	// MetadataCurrent is false when schema_migrations is absent or lacks the
	// checksum/outcome columns.
	MetadataCurrent bool
	Migrations      []MigrationEntry
	// Newer lists applied versions above every known migration, written by a
	// newer binary. They are tolerated so an older image can be rolled back to.
	Newer []string
}

// Current reports whether nothing remains for migrate to do.
func (s SchemaStatus) Current() bool {
	if !s.MetadataCurrent {
		return false
	}
	for _, migration := range s.Migrations {
		if migration.State != MigrationApplied {
			return false
		}
	}
	return true
}

func (s SchemaStatus) summary() string {
	counts := map[MigrationState]int{}
	for _, migration := range s.Migrations {
		counts[migration.State]++
	}
	parts := []string{}
	if !s.MetadataCurrent {
		parts = append(parts, "migration metadata is missing or predates checksums")
	}
	if n := counts[MigrationPending]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", n))
	}
	if n := counts[MigrationChecksumUnrecorded]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d checksums unrecorded", n))
	}
	return strings.Join(parts, ", ")
}

// Migrate takes the migration lock, re-reads the database state under it,
// bootstraps metadata if needed, adopts historical checksums and applies the
// pending migrations in order. A concurrent migrator waits for the lock and
// then finds nothing to do.
func (m Migrator) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	logger := m.logger()
	lockTimeout := m.LockTimeout
	if lockTimeout <= 0 {
		lockTimeout = DefaultMigrationLockTimeout
	}
	files, err := discoverMigrations(m.Directory)
	if err != nil {
		return err
	}
	pooled, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	conn := pooled.Hijack()
	defer func() {
		// Ending the session releases the advisory lock on success, failure
		// and cancellation alike.
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if err := acquireMigrationLock(ctx, conn, logger); err != nil {
		return err
	}
	metadata, err := readMigrationMetadata(ctx, conn)
	if err != nil {
		return err
	}
	if !metadata.current {
		if err := bootstrapMigrationMetadata(ctx, conn, lockTimeout); err != nil {
			return err
		}
		logger.Info("migration metadata bootstrapped")
		if metadata, err = readMigrationMetadata(ctx, conn); err != nil {
			return err
		}
	}
	plan, err := planMigrations(files, metadata.applied)
	if err != nil {
		return err
	}
	for _, version := range plan.status.Newer {
		logger.Warn("database has a migration newer than this binary", "version", version)
	}
	if err := adoptChecksums(ctx, conn, plan.adopt, lockTimeout); err != nil {
		return err
	}
	for _, file := range plan.adopt {
		logger.Info("migration checksum adopted", "version", file.version, "checksum", shortChecksum(file.checksum))
	}
	for _, file := range plan.pending {
		started := time.Now()
		if err := applyMigration(ctx, conn, file, lockTimeout); err != nil {
			logger.Error("migration failed", "version", file.version, "checksum", shortChecksum(file.checksum), "duration", time.Since(started))
			return err
		}
		logger.Info("migration applied", "version", file.version, "checksum", shortChecksum(file.checksum), "duration", time.Since(started))
	}
	logger.Info("migrations complete", "known", len(files), "applied", len(plan.pending), "adopted", len(plan.adopt))
	return nil
}

// Status reads the migration state. It only runs SELECT statements: it takes
// no lock, never bootstraps metadata and never executes migration SQL.
func (m Migrator) Status(ctx context.Context, pool *pgxpool.Pool) (SchemaStatus, error) {
	files, err := discoverMigrations(m.Directory)
	if err != nil {
		return SchemaStatus{}, err
	}
	metadata, err := readMigrationMetadata(ctx, pool)
	if err != nil {
		return SchemaStatus{}, err
	}
	plan, err := planMigrations(files, metadata.applied)
	if err != nil {
		return SchemaStatus{}, err
	}
	plan.status.MetadataCurrent = metadata.current
	return plan.status, nil
}

// Verify is the read-only startup check. It returns ErrSchemaNotCurrent for a
// schema that migrate can update and another error for an inconsistent one.
func (m Migrator) Verify(ctx context.Context, pool *pgxpool.Pool) error {
	status, err := m.Status(ctx, pool)
	if err != nil {
		return err
	}
	for _, version := range status.Newer {
		m.logger().Warn("database has a migration newer than this binary", "version", version)
	}
	if !status.Current() {
		return fmt.Errorf("%w: %s", ErrSchemaNotCurrent, status.summary())
	}
	return nil
}

func (m Migrator) logger() *slog.Logger {
	if m.Logger != nil {
		return m.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type migrationFile struct {
	number   uint64
	version  string
	checksum string
	body     []byte
}

var (
	migrationFilePattern    = regexp.MustCompile(`^([0-9]+_[a-z0-9]+(?:_[a-z0-9]+)*)\.(up|down)\.sql$`)
	migrationVersionPattern = regexp.MustCompile(`^([0-9]+)_[a-z0-9]+(?:_[a-z0-9]+)*$`)
)

// migrationNumber parses the numeric prefix of a version such as "001_init".
func migrationNumber(version string) (uint64, bool) {
	match := migrationVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return 0, false
	}
	number, err := strconv.ParseUint(match[1], 10, 64)
	return number, err == nil
}

// classifyMigrationFile returns the version of an up migration and "" for a
// file the runner ignores (down scripts and non-SQL files). Any other SQL
// file name is an error rather than a silently skipped migration.
func classifyMigrationFile(name string) (string, error) {
	if !strings.HasSuffix(name, ".sql") {
		return "", nil
	}
	match := migrationFilePattern.FindStringSubmatch(name)
	if match == nil {
		return "", fmt.Errorf("malformed migration file name %q: expected NNN_name.up.sql or NNN_name.down.sql", name)
	}
	if _, ok := migrationNumber(match[1]); !ok {
		return "", fmt.Errorf("migration file name %q has an out-of-range number", name)
	}
	if match[2] == "down" {
		return "", nil
	}
	return match[1], nil
}

func discoverMigrations(directory string) ([]migrationFile, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory %q: %w", directory, err)
	}
	files := []migrationFile{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, err := classifyMigrationFile(entry.Name())
		if err != nil {
			return nil, err
		}
		if version == "" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", version, err)
		}
		// Every file is checked before anything runs, so a bad file
		// executes no SQL at all.
		statement, line, err := findTransactionControl(string(body))
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", version, err)
		}
		if statement != "" {
			return nil, fmt.Errorf("migration %s: line %d: %s is transaction control; each migration already runs in its own transaction", version, line, statement)
		}
		number, _ := migrationNumber(version)
		sum := sha256.Sum256(body)
		files = append(files, migrationFile{number: number, version: version, checksum: hex.EncodeToString(sum[:]), body: body})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migrations found in %q", directory)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].number < files[j].number })
	for i := 1; i < len(files); i++ {
		if files[i].number == files[i-1].number {
			return nil, fmt.Errorf("migrations %s and %s share number %d", files[i-1].version, files[i].version, files[i].number)
		}
	}
	return files, nil
}

type appliedMigration struct {
	version string
	// checksum is "" when the row was written before checksums were recorded.
	checksum string
	outcome  string
}

type migrationPlan struct {
	status  SchemaStatus
	adopt   []migrationFile
	pending []migrationFile
}

// planMigrations compares the known files with the applied rows. It rejects
// checksum mismatches, unknown versions inside the known range, pending
// migrations below an applied version, and unsupported metadata.
func planMigrations(files []migrationFile, applied []appliedMigration) (migrationPlan, error) {
	known := make(map[string]migrationFile, len(files))
	byNumber := make(map[uint64]migrationFile, len(files))
	var maxKnown uint64
	for _, file := range files {
		known[file.version] = file
		byNumber[file.number] = file
		maxKnown = max(maxKnown, file.number)
	}
	recorded := make(map[string]appliedMigration, len(applied))
	var highest string
	var highestNumber uint64
	type newerVersion struct {
		number  uint64
		version string
	}
	newer := []newerVersion{}
	for _, row := range applied {
		number, ok := migrationNumber(row.version)
		if !ok {
			return migrationPlan{}, fmt.Errorf("schema_migrations contains unrecognized version %q", row.version)
		}
		if row.outcome != "applied" {
			return migrationPlan{}, fmt.Errorf("migration %s has unsupported outcome %q", row.version, row.outcome)
		}
		if highest == "" || number > highestNumber {
			highest, highestNumber = row.version, number
		}
		if file, ok := known[row.version]; ok {
			switch {
			case row.checksum == "":
				historical, ok := historicalChecksums[row.version]
				if !ok {
					return migrationPlan{}, fmt.Errorf("migration %s has no recorded checksum and is not a historical migration", row.version)
				}
				if historical != file.checksum {
					return migrationPlan{}, fmt.Errorf("migration %s file checksum %s differs from the historical checksum %s", row.version, shortChecksum(file.checksum), shortChecksum(historical))
				}
			case row.checksum != file.checksum:
				return migrationPlan{}, fmt.Errorf("migration %s checksum mismatch: applied %s, file %s", row.version, shortChecksum(row.checksum), shortChecksum(file.checksum))
			}
			recorded[row.version] = row
			continue
		}
		if file, ok := byNumber[number]; ok {
			return migrationPlan{}, fmt.Errorf("applied migration %s conflicts with migration file %s", row.version, file.version)
		}
		if number < maxKnown {
			return migrationPlan{}, fmt.Errorf("applied migration %s is not in this binary's migration set", row.version)
		}
		newer = append(newer, newerVersion{number, row.version})
	}
	plan := migrationPlan{status: SchemaStatus{MetadataCurrent: true, Migrations: make([]MigrationEntry, 0, len(files)), Newer: []string{}}}
	for _, file := range files {
		entry := MigrationEntry{Version: file.version, Checksum: file.checksum, State: MigrationApplied}
		row, ok := recorded[file.version]
		switch {
		case ok && row.checksum == "":
			entry.State = MigrationChecksumUnrecorded
			plan.adopt = append(plan.adopt, file)
		case !ok:
			if highest != "" && file.number < highestNumber {
				return migrationPlan{}, fmt.Errorf("migration %s is pending but later migration %s is already applied", file.version, highest)
			}
			entry.State = MigrationPending
			plan.pending = append(plan.pending, file)
		}
		plan.status.Migrations = append(plan.status.Migrations, entry)
	}
	sort.Slice(newer, func(i, j int) bool { return newer[i].number < newer[j].number })
	for _, version := range newer {
		plan.status.Newer = append(plan.status.Newer, version.version)
	}
	return plan, nil
}

type migrationQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type migrationMetadata struct {
	// current means schema_migrations exists with checksum and outcome.
	current bool
	applied []appliedMigration
}

func readMigrationMetadata(ctx context.Context, q migrationQuerier) (migrationMetadata, error) {
	var exists, checksum, outcome bool
	err := q.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL,
		EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('schema_migrations') AND attname='checksum' AND NOT attisdropped),
		EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('schema_migrations') AND attname='outcome' AND NOT attisdropped)`).Scan(&exists, &checksum, &outcome)
	if err != nil {
		return migrationMetadata{}, fmt.Errorf("inspect migration metadata: %w", err)
	}
	metadata := migrationMetadata{current: exists && checksum && outcome, applied: []appliedMigration{}}
	if !exists {
		return metadata, nil
	}
	query := `SELECT version,'','applied' FROM schema_migrations`
	if metadata.current {
		query = `SELECT version,COALESCE(checksum,''),outcome FROM schema_migrations`
	}
	rows, err := q.Query(ctx, query)
	if err != nil {
		return migrationMetadata{}, fmt.Errorf("read migration metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row appliedMigration
		if err := rows.Scan(&row.version, &row.checksum, &row.outcome); err != nil {
			return migrationMetadata{}, fmt.Errorf("scan migration metadata: %w", err)
		}
		metadata.applied = append(metadata.applied, row)
	}
	if err := rows.Err(); err != nil {
		return migrationMetadata{}, fmt.Errorf("read migration metadata: %w", err)
	}
	return metadata, nil
}

// bootstrapMigrationMetadata creates or additively expands schema_migrations.
// It is idempotent and runs only under the migration lock.
func bootstrapMigrationMetadata(ctx context.Context, conn *pgx.Conn, lockTimeout time.Duration) error {
	return inMigrationTx(ctx, conn, lockTimeout, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return fmt.Errorf("create migration metadata: %w", err)
		}
		// Column constraints are added only together with their column, so a
		// rerun adds nothing.
		if _, err := tx.Exec(ctx, `ALTER TABLE schema_migrations
			ADD COLUMN IF NOT EXISTS checksum text CONSTRAINT schema_migrations_checksum_format CHECK (checksum ~ '^[0-9a-f]{64}$'),
			ADD COLUMN IF NOT EXISTS outcome text NOT NULL DEFAULT 'applied' CONSTRAINT schema_migrations_outcome_known CHECK (outcome IN ('applied'))`); err != nil {
			return fmt.Errorf("expand migration metadata: %w", err)
		}
		return nil
	})
}

// adoptChecksums records verified historical checksums. The IS NULL guard
// means a recorded checksum is never overwritten.
func adoptChecksums(ctx context.Context, conn *pgx.Conn, files []migrationFile, lockTimeout time.Duration) error {
	if len(files) == 0 {
		return nil
	}
	return inMigrationTx(ctx, conn, lockTimeout, func(tx pgx.Tx) error {
		for _, file := range files {
			tag, err := tx.Exec(ctx, `UPDATE schema_migrations SET checksum=$2 WHERE version=$1 AND checksum IS NULL`, file.version, file.checksum)
			if err != nil {
				return fmt.Errorf("adopt checksum for %s: %w", file.version, err)
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("adopt checksum for %s: metadata changed during migration", file.version)
			}
		}
		return nil
	})
}

// applyMigration runs one file and records it in the same transaction.
// discoverMigrations has already rejected files with transaction control.
func applyMigration(ctx context.Context, conn *pgx.Conn, file migrationFile, lockTimeout time.Duration) error {
	return inMigrationTx(ctx, conn, lockTimeout, func(tx pgx.Tx) error {
		// The server parses the whole file with this session's setting before
		// running any of it; the preflight lexes strings the same way only if
		// it is on. An earlier file in this run could have changed it.
		var conforming string
		if err := tx.QueryRow(ctx, `SELECT current_setting('standard_conforming_strings')`).Scan(&conforming); err != nil {
			return fmt.Errorf("apply migration %s: read standard_conforming_strings: %w", file.version, err)
		}
		if conforming != "on" {
			return fmt.Errorf("apply migration %s: standard_conforming_strings must be on", file.version)
		}
		// The simple protocol runs the file's several statements.
		if _, err := tx.Conn().PgConn().Exec(ctx, string(file.body)).ReadAll(); err != nil {
			return fmt.Errorf("apply migration %s: %w", file.version, err)
		}
		// Defense in depth behind the preflight.
		if status := tx.Conn().PgConn().TxStatus(); status != 'T' {
			return fmt.Errorf("apply migration %s: the file ended its transaction (status %q); migrations must not contain transaction control", file.version, status)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum,outcome) VALUES($1,$2,'applied')`, file.version, file.checksum); err != nil {
			return fmt.Errorf("record migration %s: %w", file.version, err)
		}
		return nil
	})
}

func inMigrationTx(ctx context.Context, conn *pgx.Conn, lockTimeout time.Duration, work func(pgx.Tx) error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	// SET cannot take parameters; the value is a formatted integer.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL lock_timeout = %d", max(1, lockTimeout.Milliseconds()))); err != nil {
		return fmt.Errorf("set migration lock timeout: %w", err)
	}
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	return nil
}

// acquireMigrationLock polls pg_try_advisory_lock instead of blocking in
// pg_advisory_lock: pgx cancels by breaking the socket, which would leave a
// blocked backend queued for the lock. Between polls no query is in flight,
// so cancellation is clean.
func acquireMigrationLock(ctx context.Context, conn *pgx.Conn, logger *slog.Logger) error {
	started := time.Now()
	waiting := false
	for {
		var locked bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, MigrationLockKey).Scan(&locked); err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		if locked {
			if waiting {
				logger.Info("migration lock acquired", "waited", time.Since(started))
			}
			return nil
		}
		if !waiting {
			logger.Info("waiting for the migration lock held by another migrator", "lockKey", MigrationLockKey)
			waiting = true
		}
		timer := time.NewTimer(migrationLockPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for migration lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func shortChecksum(checksum string) string {
	if len(checksum) > 12 {
		return checksum[:12]
	}
	return checksum
}

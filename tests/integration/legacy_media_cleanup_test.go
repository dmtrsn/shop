//go:build integration

package integration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sweetshop/internal/repository/postgres"
)

// The tracer observes the actual cleanup context, then injects an already
// expired deadline to exercise a non-responsive unlock without a real wait.
type legacyCleanupTracer struct {
	t       *testing.T
	timeout bool
	fail    bool
	seen    bool
	conn    *pgx.Conn
}

func (tr *legacyCleanupTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL != "SELECT pg_advisory_unlock($1)" {
		return ctx
	}
	tr.seen, tr.conn = true, conn
	if ctx.Err() != nil {
		tr.t.Errorf("cleanup inherited cancelled operation: %v", ctx.Err())
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		tr.t.Error("cleanup unlock has no short bounded deadline")
	}
	if tr.timeout {
		expired, cancel := context.WithDeadline(ctx, time.Unix(1, 0))
		cancel()
		return expired
	}
	if tr.fail {
		// Leave an open session holding the advisory lock, but make the
		// unlock return a server error from an aborted transaction.
		if _, err := conn.PgConn().Exec(ctx, "BEGIN; SELECT 1/0").ReadAll(); err == nil {
			tr.t.Error("failed to inject aborted transaction")
		}
	}
	return ctx
}

func (*legacyCleanupTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestLegacyMediaCleanupAfterCancellation(t *testing.T) {
	d := newDatabase(t, false)
	for _, outcome := range []string{"success", "timeout", "server error"} {
		t.Run(outcome, func(t *testing.T) {
			failed := outcome != "success"
			tracer := &legacyCleanupTracer{t: t, timeout: outcome == "timeout", fail: outcome == "server error"}
			config := d.pool.Config()
			config.MaxConns = 1
			config.ConnConfig.Tracer = tracer
			pool, err := pgxpool.NewWithConfig(d.ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			ctx, cancel := context.WithCancel(d.ctx)
			defer cancel()
			release, err := postgres.New(pool).LockLegacyMigration(ctx)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			err = release()
			if (err != nil) != failed {
				t.Fatalf("cleanup error=%v; failure expected=%v", err, failed)
			}
			if tracer.timeout && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline error lost: %v", err)
			}
			if tracer.fail && !strings.Contains(err.Error(), "release legacy migration lock") {
				t.Fatalf("missing cleanup error context: %v", err)
			}
			if !tracer.seen {
				t.Fatal("unlock did not run")
			}
			if failed && !tracer.conn.IsClosed() {
				t.Fatal("uncertain session was not closed")
			}
			if !failed && tracer.conn.IsClosed() {
				t.Fatal("successful unlock discarded a healthy connection")
			}
			if pool.Stat().AcquiredConns() != 0 || (failed && pool.Stat().TotalConns() != 0) {
				t.Fatalf("cleanup retained the connection: %v", pool.Stat())
			}
			// Wait for the backend to observe the closed socket before checking
			// the nonblocking run lock. No scheduling sleep is needed.
			proof, err := d.pool.Acquire(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer proof.Release()
			const key int64 = 0x73776565746c6763
			if _, err := proof.Exec(d.ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
				t.Fatalf("lock leaked after cleanup: %v", err)
			}
			if _, err := proof.Exec(d.ctx, "SELECT pg_advisory_unlock($1)", key); err != nil {
				t.Fatal(err)
			}
			// A different pool must be able to take the session-level lock.
			again, err := d.store.LockLegacyMigration(d.ctx)
			if err != nil {
				t.Fatalf("lock leaked after cleanup: %v", err)
			}
			if err := again(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

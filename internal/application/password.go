package application

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
)

// passwordGuard bounds concurrent bcrypt work in this process. Anonymous
// requests (sign-in, registration, confirmation) each cost one bcrypt
// operation of about a quarter second, so without a bound a burst would
// occupy every CPU. A caller runs when a slot is free, otherwise waits at
// most wait while at most queue others wait too; beyond that it gets
// domain.ErrBusy at once. The guard starts no goroutines: waiters are the
// request goroutines themselves, and their number is capped by queue.
type passwordGuard struct {
	slots    chan struct{}
	admitted atomic.Int64
	limit    int64
	wait     time.Duration
}

func newPasswordGuard(slots, queue int, wait time.Duration) *passwordGuard {
	slots = max(1, slots)
	return &passwordGuard{slots: make(chan struct{}, slots), limit: int64(slots + max(0, queue)), wait: wait}
}

// passwords is the process-wide guard: one bcrypt operation per CPU, four
// waiters per CPU, and a one-second wait (about four operations' time).
var passwords = newPasswordGuard(runtime.GOMAXPROCS(0), 4*runtime.GOMAXPROCS(0), time.Second)

func (g *passwordGuard) run(ctx context.Context, work func()) error {
	if g.admitted.Add(1) > g.limit {
		g.admitted.Add(-1)
		return domain.ErrBusy
	}
	defer g.admitted.Add(-1)
	select {
	case g.slots <- struct{}{}:
	default:
		timer := time.NewTimer(g.wait)
		defer timer.Stop()
		select {
		case g.slots <- struct{}{}:
		case <-timer.C:
			return domain.ErrBusy
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { <-g.slots }()
	work()
	return nil
}

// hashPassword is bcrypt.GenerateFromPassword under the guard.
func hashPassword(ctx context.Context, password string, cost int) (string, error) {
	var hash []byte
	var err error
	if guardErr := passwords.run(ctx, func() { hash, err = bcrypt.GenerateFromPassword([]byte(password), cost) }); guardErr != nil {
		return "", guardErr
	}
	return string(hash), err
}

// passwordMatches is bcrypt.CompareHashAndPassword under the guard. A
// mismatch, a malformed hash and an over-long password are all false; only
// the guard (ErrBusy, cancellation) returns an error.
func passwordMatches(ctx context.Context, hash, password string) (bool, error) {
	var compareErr error
	if err := passwords.run(ctx, func() { compareErr = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) }); err != nil {
		return false, err
	}
	return compareErr == nil, nil
}

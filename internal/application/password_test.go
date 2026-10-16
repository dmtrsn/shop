package application

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

func TestPasswordGuardBoundsConcurrentWork(t *testing.T) {
	guard := newPasswordGuard(2, 4, 5*time.Second)
	release := make(chan struct{})
	var running, peak atomic.Int64
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- guard.run(context.Background(), func() {
				now := running.Add(1)
				for {
					old := peak.Load()
					if now <= old || peak.CompareAndSwap(old, now) {
						break
					}
				}
				<-release
				running.Add(-1)
			})
		}()
	}
	waitFor(t, func() bool { return guard.admitted.Load() == 6 })
	// Two run, four wait; a seventh caller is refused at once.
	started := time.Now()
	if err := guard.run(context.Background(), func() { t.Error("overflow caller ran") }); !errors.Is(err, domain.ErrBusy) || time.Since(started) > time.Second {
		t.Fatalf("overflow: %v after %v", err, time.Since(started))
	}
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if peak.Load() != 2 || guard.admitted.Load() != 0 || len(guard.slots) != 0 {
		t.Fatalf("peak %d admitted %d held %d", peak.Load(), guard.admitted.Load(), len(guard.slots))
	}
}

func TestPasswordGuardWaitIsBounded(t *testing.T) {
	guard := newPasswordGuard(1, 1, 50*time.Millisecond)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = guard.run(context.Background(), func() { <-release })
	}()
	waitFor(t, func() bool { return len(guard.slots) == 1 })
	started := time.Now()
	if err := guard.run(context.Background(), func() {}); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("waited past the bound: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("wait %v", elapsed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := newPasswordGuard(1, 1, time.Hour)
	slow.slots <- struct{}{}
	if err := slow.run(ctx, func() {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	close(release)
	<-done
}

// An unknown address and a real account take the same guarded path: with
// the guard saturated both are refused alike, so an unknown address is never
// answered faster by skipping bcrypt.
func TestLoginUsesTheGuardForUnknownAndKnownAccounts(t *testing.T) {
	saved := passwords
	t.Cleanup(func() { passwords = saved })
	passwords = newPasswordGuard(1, 0, 10*time.Millisecond)
	passwords.slots <- struct{}{}
	service := New(memory.NewDemo(), Options{AllowDemoAccount: true})
	for _, address := range []string{"demo@sugar.ru", "nobody@example.com"} {
		if _, err := service.StartCookieSession(context.Background(), address, "Sweet2026!", ""); !errors.Is(err, domain.ErrBusy) {
			t.Fatalf("%s: %v", address, err)
		}
	}
	<-passwords.slots
	if _, err := service.StartCookieSession(context.Background(), "nobody@example.com", "Sweet2026!", ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("unknown address with a free guard: %v", err)
	}
	if _, err := service.StartCookieSession(context.Background(), "demo@sugar.ru", "Sweet2026!", ""); err != nil {
		t.Fatalf("known address with a free guard: %v", err)
	}
}

func TestDefaultPasswordGuardFollowsTheCPUCount(t *testing.T) {
	if cap(passwords.slots) != runtime.GOMAXPROCS(0) || passwords.limit != int64(5*runtime.GOMAXPROCS(0)) || passwords.wait != time.Second {
		t.Fatalf("slots %d limit %d wait %v", cap(passwords.slots), passwords.limit, passwords.wait)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

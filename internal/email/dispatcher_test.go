package email_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"sweetshop/internal/email"
	"sweetshop/internal/email/emailtest"
)

// syncBuffer is a log sink shared with worker goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func fastOptions() email.DispatcherOptions {
	return email.DispatcherOptions{QueueSize: 4, Workers: 2, Attempts: 3, Backoff: []time.Duration{5 * time.Millisecond, 10 * time.Millisecond},
		SendTimeout: time.Second, RatePerMinute: 60000, Burst: 100, AbandonAfter: time.Second}
}

func secretMessage(t *testing.T, ref string) email.Message {
	t.Helper()
	m, err := email.PasswordReset("ann@example.com", ref, secretLinkReset(t), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func secretLinkReset(t *testing.T) email.Link {
	t.Helper()
	link, err := origin(t).Link("/reset-password", "t="+secretToken)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

func newDispatcher(t *testing.T, mailer email.Mailer, options email.DispatcherOptions) (*email.Dispatcher, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	d, err := email.NewDispatcher(mailer, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})), options)
	if err != nil {
		t.Fatal(err)
	}
	return d, logs
}

func assertNoSecrets(t *testing.T, logs string) {
	t.Helper()
	for _, secret := range []string{secretToken, "ann@example.com", "reset-password", "Восстановление", "Здравствуйте"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs leak %q:\n%s", secret, logs)
		}
	}
}

func waitUntil(t *testing.T, label string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestDispatcherDeliversAndShutsDown(t *testing.T) {
	recorder := &emailtest.Recorder{}
	d, logs := newDispatcher(t, recorder, fastOptions())
	d.Start()
	d.Start() // idempotent
	for _, ref := range []string{"a", "b", "c"} {
		if err := d.Enqueue(secretMessage(t, ref)); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Delivered()) != 3 {
		t.Fatalf("delivered %d", len(recorder.Delivered()))
	}
	if err := d.Enqueue(secretMessage(t, "late")); !errors.Is(err, email.ErrDispatcherClosed) {
		t.Fatalf("enqueue after shutdown: %v", err)
	}
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatal("second shutdown")
	}
	if strings.Count(logs.String(), `msg="email delivered"`) != 3 || !strings.Contains(logs.String(), "dispatcher is shut down") {
		t.Fatalf("logs:\n%s", logs.String())
	}
	assertNoSecrets(t, logs.String())
}

func TestDispatcherQueueFullIsVisible(t *testing.T) {
	block := make(chan struct{})
	recorder := &emailtest.Recorder{Block: block}
	options := fastOptions()
	options.QueueSize, options.Workers = 2, 1
	d, logs := newDispatcher(t, recorder, options)
	d.Start()
	accepted, full := 0, 0
	for i := 0; i < 10; i++ {
		switch err := d.Enqueue(secretMessage(t, "r")); {
		case err == nil:
			accepted++
		case errors.Is(err, email.ErrQueueFull):
			full++
		default:
			t.Fatal(err)
		}
	}
	// One message is held by the blocked worker, two wait in the queue.
	if accepted > 3 || accepted < 2 || full != 10-accepted {
		t.Fatalf("accepted=%d full=%d", accepted, full)
	}
	if strings.Count(logs.String(), "queue is full") != full {
		t.Fatalf("queue-full drops not logged:\n%s", logs.String())
	}
	close(block)
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Delivered()) != accepted {
		t.Fatalf("delivered %d of %d", len(recorder.Delivered()), accepted)
	}
	assertNoSecrets(t, logs.String())
}

func TestDispatcherRetriesOnlyTemporaryFailures(t *testing.T) {
	transient := email.Temporary(&email.SMTPError{Stage: "rcpt", Code: 451})
	permanent := &email.SMTPError{Stage: "rcpt", Code: 550}
	for name, tc := range map[string]struct {
		fail     func(attempt int, m email.Message) error
		attempts int
		ok       bool
	}{
		"temporary then success":    {func(a int, _ email.Message) error { return map[bool]error{true: transient}[a < 3] }, 3, true},
		"temporary exhausted":       {func(int, email.Message) error { return transient }, 3, false},
		"permanent not retried":     {func(int, email.Message) error { return permanent }, 1, false},
		"unclassified is permanent": {func(int, email.Message) error { return errors.New("x509: certificate signed by unknown authority") }, 1, false},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &emailtest.Recorder{Fail: tc.fail}
			d, logs := newDispatcher(t, recorder, fastOptions())
			d.Start()
			if err := d.Enqueue(secretMessage(t, "usr_1")); err != nil {
				t.Fatal(err)
			}
			if err := d.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(recorder.Attempts()) != tc.attempts || (len(recorder.Delivered()) == 1) != tc.ok {
				t.Fatalf("attempts=%d delivered=%d", len(recorder.Attempts()), len(recorder.Delivered()))
			}
			if !tc.ok && !strings.Contains(logs.String(), `msg="email delivery failed"`) {
				t.Fatal("failure not logged")
			}
			assertNoSecrets(t, logs.String())
		})
	}
}

func TestDispatcherShutdownDeadline(t *testing.T) {
	block := make(chan struct{}) // never closed: sends end only by cancellation
	recorder := &emailtest.Recorder{Block: block}
	options := fastOptions()
	options.Workers, options.QueueSize = 1, 3
	d, logs := newDispatcher(t, recorder, options)
	d.Start()
	for i := 0; i < 3; i++ {
		if err := d.Enqueue(secretMessage(t, "r")); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := d.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("shutdown: %v after %v", err, time.Since(started))
	}
	if !strings.Contains(err.Error(), "messages dropped") || !strings.Contains(logs.String(), `msg="email dropped"`) {
		t.Fatalf("drops not reported: %v\n%s", err, logs.String())
	}
	assertNoSecrets(t, logs.String()+err.Error())
}

func TestDispatcherGlobalCap(t *testing.T) {
	recorder := &emailtest.Recorder{}
	options := fastOptions()
	options.RatePerMinute, options.Burst = 600, 1 // one every 100 ms after the first
	d, _ := newDispatcher(t, recorder, options)
	d.Start()
	started := time.Now()
	for i := 0; i < 4; i++ {
		if err := d.Enqueue(secretMessage(t, "r")); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); len(recorder.Delivered()) != 4 || elapsed < 280*time.Millisecond {
		t.Fatalf("4 messages in %v: the cap did not hold", elapsed)
	}
}

func TestDispatcherRejectsInvalidInput(t *testing.T) {
	if _, err := email.NewDispatcher(nil, slog.Default(), fastOptions()); err == nil {
		t.Fatal("nil mailer")
	}
	bad := fastOptions()
	bad.QueueSize = 0
	if _, err := email.NewDispatcher(&emailtest.Recorder{}, slog.Default(), bad); err == nil {
		t.Fatal("unbounded queue options accepted")
	}
	d, logs := newDispatcher(t, &emailtest.Recorder{}, fastOptions())
	if err := d.Enqueue(email.Message{Kind: email.KindTest, To: "ann@example.com\r\nBcc: x@evil.example", Subject: "s", Body: "b"}); !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatal("invalid message queued")
	}
	if strings.Contains(logs.String(), "evil") {
		t.Fatal("rejected recipient logged")
	}
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatcherLeavesNoGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		d, _ := newDispatcher(t, &emailtest.Recorder{}, fastOptions())
		d.Start()
		_ = d.Enqueue(secretMessage(t, "r"))
		if err := d.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, "goroutines to exit", func() bool { return runtime.NumGoroutine() <= before+1 })
}

func TestLogMailerNeverWritesTheMessage(t *testing.T) {
	logs := &syncBuffer{}
	mailer := email.LogMailer{Logger: slog.New(slog.NewTextHandler(logs, nil))}
	if err := mailer.Send(context.Background(), secretMessage(t, "usr_1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "kind=password_reset") || !strings.Contains(logs.String(), "recipient=<redacted>") {
		t.Fatalf("log transport output: %s", logs.String())
	}
	assertNoSecrets(t, logs.String())
}

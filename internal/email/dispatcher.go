package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrQueueFull: the bounded queue is full and the message was dropped.
	// The caller decides what to tell the customer; the drop is logged.
	ErrQueueFull = errors.New("email queue is full")
	// ErrDispatcherClosed: Shutdown has begun; the message was not accepted.
	ErrDispatcherClosed = errors.New("email dispatcher is shut down")
)

// DispatcherOptions bound memory, concurrency, retries and the send rate.
type DispatcherOptions struct {
	QueueSize int
	Workers   int
	// Attempts is the total number of tries per message, including the first.
	Attempts int
	// Backoff[i] is the wait before try i+2; the last value repeats.
	Backoff     []time.Duration
	SendTimeout time.Duration
	// RatePerMinute and Burst form a process-wide token bucket. Each replica
	// has its own; it backs up, and does not replace, per-address and
	// per-client limits.
	RatePerMinute int
	Burst         int
	// AbandonAfter bounds how long Shutdown waits for workers once the drain
	// deadline has passed and in-flight sends are cancelled.
	AbandonAfter time.Duration
}

// DefaultDispatcherOptions: 100 queued messages, 2 workers, 3 tries
// (retries after 2 s and 10 s), 20 s per try, 60 messages per minute with a
// burst of 10.
func DefaultDispatcherOptions() DispatcherOptions {
	return DispatcherOptions{
		QueueSize: 100, Workers: 2, Attempts: 3, Backoff: []time.Duration{2 * time.Second, 10 * time.Second},
		SendTimeout: 20 * time.Second, RatePerMinute: 60, Burst: 10, AbandonAfter: 5 * time.Second,
	}
}

// Dispatcher delivers queued messages in the background. Delivery is
// best-effort and in memory only: anything queued or in flight is lost if the
// process stops. It is not durable and not exactly-once.
type Dispatcher struct {
	mailer  Mailer
	logger  *slog.Logger
	options DispatcherOptions
	limiter *rateLimiter

	mu      sync.RWMutex
	closed  bool
	queue   chan Message
	started sync.Once
	workers sync.WaitGroup

	work    context.Context
	stop    context.CancelFunc
	dropped atomic.Int64
}

func NewDispatcher(mailer Mailer, logger *slog.Logger, options DispatcherOptions) (*Dispatcher, error) {
	switch {
	case mailer == nil:
		return nil, errors.New("email dispatcher needs a mailer")
	case options.QueueSize < 1 || options.Workers < 1 || options.Attempts < 1 || options.SendTimeout <= 0 ||
		options.RatePerMinute < 1 || options.Burst < 1 || options.AbandonAfter <= 0:
		return nil, errors.New("email dispatcher options must be positive")
	}
	for _, delay := range options.Backoff {
		if delay <= 0 {
			return nil, errors.New("email dispatcher backoff must be positive")
		}
	}
	work, stop := context.WithCancel(context.Background())
	return &Dispatcher{
		mailer: mailer, logger: logger, options: options, limiter: newRateLimiter(options.RatePerMinute, options.Burst),
		queue: make(chan Message, options.QueueSize), work: work, stop: stop,
	}, nil
}

// Start launches the workers once.
func (d *Dispatcher) Start() {
	d.started.Do(func() {
		for range d.options.Workers {
			d.workers.Add(1)
			go d.run()
		}
	})
}

// Enqueue accepts a validated message without blocking. It returns
// ErrQueueFull or ErrDispatcherClosed when the message is not accepted.
func (d *Dispatcher) Enqueue(message Message) error {
	if err := message.Validate(); err != nil {
		d.logger.Error("email rejected", "kind", message.Kind, "ref", message.Ref, "error", err)
		return err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		d.logger.Error("email not accepted: dispatcher is shut down", "kind", message.Kind, "ref", message.Ref)
		return ErrDispatcherClosed
	}
	select {
	case d.queue <- message:
		return nil
	default:
		d.logger.Error("email dropped: queue is full", "kind", message.Kind, "ref", message.Ref, "capacity", d.options.QueueSize)
		return ErrQueueFull
	}
}

// Shutdown stops accepting messages and delivers what is queued until ctx is
// done. Then it cancels in-flight sends, drops the rest, and waits at most
// AbandonAfter for the workers.
func (d *Dispatcher) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.queue)
	}
	d.mu.Unlock()
	d.Start() // a never-started dispatcher still drains (or drops) its queue
	done := make(chan struct{})
	go func() {
		d.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	d.stop()
	select {
	case <-done:
	case <-time.After(d.options.AbandonAfter):
		d.logger.Error("email workers did not stop; abandoning them")
	}
	return fmt.Errorf("email dispatcher drain: %w (%d messages dropped)", ctx.Err(), d.dropped.Load())
}

func (d *Dispatcher) run() {
	defer d.workers.Done()
	for message := range d.queue {
		if d.work.Err() != nil {
			d.drop(message, "shutdown deadline passed")
			continue
		}
		d.deliver(message)
	}
}

func (d *Dispatcher) deliver(message Message) {
	for attempt := 1; ; attempt++ {
		if err := d.limiter.wait(d.work); err != nil {
			d.drop(message, "shutdown deadline passed")
			return
		}
		ctx, cancel := context.WithTimeout(d.work, d.options.SendTimeout)
		err := d.mailer.Send(ctx, message)
		cancel()
		if err == nil {
			d.logger.Info("email delivered", "kind", message.Kind, "ref", message.Ref, "attempt", attempt)
			return
		}
		temporary := IsTemporary(err)
		if !temporary || attempt >= d.options.Attempts || d.work.Err() != nil {
			d.logger.Error("email delivery failed", "kind", message.Kind, "ref", message.Ref, "attempt", attempt, "temporary", temporary, "error", err)
			return
		}
		d.logger.Warn("email delivery will be retried", "kind", message.Kind, "ref", message.Ref, "attempt", attempt, "error", err)
		delay := d.options.Backoff[min(attempt-1, len(d.options.Backoff)-1)]
		timer := time.NewTimer(delay)
		select {
		case <-d.work.Done():
			timer.Stop()
			d.drop(message, "shutdown deadline passed")
			return
		case <-timer.C:
		}
	}
}

func (d *Dispatcher) drop(message Message, reason string) {
	d.dropped.Add(1)
	d.logger.Error("email dropped", "kind", message.Kind, "ref", message.Ref, "reason", reason)
}

// rateLimiter is a token bucket shared by the workers.
type rateLimiter struct {
	mu        sync.Mutex
	tokens    float64
	burst     float64
	perSecond float64
	last      time.Time
}

func newRateLimiter(perMinute, burst int) *rateLimiter {
	return &rateLimiter{tokens: float64(burst), burst: float64(burst), perSecond: float64(perMinute) / 60, last: time.Now()}
}

func (l *rateLimiter) wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens = min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.perSecond)
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - l.tokens) / l.perSecond * float64(time.Second))
		l.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

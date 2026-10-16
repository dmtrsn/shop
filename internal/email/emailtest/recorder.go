// Package emailtest provides a capturing Mailer for tests.
package emailtest

import (
	"context"
	"sync"

	"sweetshop/internal/email"
)

// Recorder records every Send attempt in order. Fail, when set, decides the
// outcome of each attempt (1-based across all messages); a nil result
// delivers. Block, when set, holds every attempt until it is closed or the
// context ends.
type Recorder struct {
	mu        sync.Mutex
	attempts  []email.Message
	delivered []email.Message
	Fail      func(attempt int, message email.Message) error
	Block     chan struct{}
}

func (r *Recorder) Send(ctx context.Context, message email.Message) error {
	if r.Block != nil {
		select {
		case <-r.Block:
		case <-ctx.Done():
			return email.Temporary(ctx.Err())
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := message.Validate(); err != nil {
		return err
	}
	r.attempts = append(r.attempts, message)
	if r.Fail != nil {
		if err := r.Fail(len(r.attempts), message); err != nil {
			return err
		}
	}
	r.delivered = append(r.delivered, message)
	return nil
}

// Attempts returns every attempted message in call order.
func (r *Recorder) Attempts() []email.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]email.Message(nil), r.attempts...)
}

// Delivered returns the messages whose attempt succeeded, in order.
func (r *Recorder) Delivered() []email.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]email.Message(nil), r.delivered...)
}

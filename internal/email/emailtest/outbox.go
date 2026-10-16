package emailtest

import (
	"regexp"
	"sync"

	"sweetshop/internal/email"
)

// Outbox records enqueued messages synchronously, in place of a Dispatcher.
// Refuse, when set, is returned instead of accepting, like a full queue.
type Outbox struct {
	mu       sync.Mutex
	accepted []email.Message
	refused  int
	Refuse   error
}

func (o *Outbox) Enqueue(message email.Message) error {
	if err := message.Validate(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.Refuse != nil {
		o.refused++
		return o.Refuse
	}
	o.accepted = append(o.accepted, message)
	return nil
}

// Messages returns the accepted messages in order.
func (o *Outbox) Messages() []email.Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]email.Message(nil), o.accepted...)
}

// To returns the accepted messages for one recipient.
func (o *Outbox) To(recipient string) []email.Message {
	result := []email.Message{}
	for _, message := range o.Messages() {
		if message.To == recipient {
			result = append(result, message)
		}
	}
	return result
}

// Refused counts messages the outbox turned away.
func (o *Outbox) Refused() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.refused
}

var fragmentToken = regexp.MustCompile(`#t=([A-Za-z0-9_-]+)`)

// Token extracts the fragment token of the link in a message body, or "".
func Token(message email.Message) string {
	if match := fragmentToken.FindStringSubmatch(message.Body); match != nil {
		return match[1]
	}
	return ""
}

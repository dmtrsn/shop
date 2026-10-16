// Package email delivers transactional email: a small Mailer interface, an
// SMTP transport that requires TLS in production, fixed plain-text templates,
// and a bounded in-process dispatcher.
//
// Delivery is not durable. A queued message lives only in process memory and
// is lost if the process stops; the recovery model is that the customer asks
// again. Messages may carry secret links, so nothing in this package logs a
// recipient, subject or body, and Message redacts itself when formatted.
package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"sweetshop/internal/domain"
)

// Mailer sends one message. Implementations return errors that IsTemporary
// classifies; any other error is permanent.
type Mailer interface {
	Send(ctx context.Context, message Message) error
}

// Kind names a template. It is the only message detail that is logged.
type Kind string

const (
	KindRegistrationConfirmation Kind = "registration_confirmation"
	KindAccountExists            Kind = "account_exists"
	KindPasswordReset            Kind = "password_reset"
	KindTest                     Kind = "test"
)

const maxSubjectRunes = 200

// Message is one plain-text UTF-8 email. Build it with the template
// functions; they validate it.
type Message struct {
	Kind Kind
	// Ref is an internal reference for logs, such as an account ID. Never the
	// recipient.
	Ref string
	// To is a normalized address (domain.NormalizeEmail).
	To      string
	Subject string
	// Body may contain secret links and is never logged.
	Body string
}

// String, GoString and LogValue redact the message, so formatting it in a
// log line, error or panic cannot leak the recipient, subject or body.
func (m Message) String() string {
	return fmt.Sprintf("email.Message{Kind:%s Ref:%s To:<redacted> Subject:<redacted> Body:<redacted>}", m.Kind, m.Ref)
}

func (m Message) GoString() string { return m.String() }

func (m Message) LogValue() slog.Value {
	return slog.GroupValue(slog.String("kind", string(m.Kind)), slog.String("ref", m.Ref))
}

// ErrInvalidMessage rejects a message that could alter SMTP headers or is
// not deliverable as UTF-8 text. It is permanent.
var ErrInvalidMessage = errors.New("invalid email message")

// Validate checks every field that becomes an SMTP header or command. Errors
// never quote the field values.
func (m Message) Validate() error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidMessage, reason) }
	switch {
	case m.Kind == "" || !safeToken(string(m.Kind)):
		return invalid("kind")
	case m.Ref != "" && !safeToken(m.Ref):
		return invalid("ref")
	}
	if normalized, ok := domain.NormalizeEmail(m.To); !ok || normalized != m.To {
		return invalid("recipient must be a normalized address")
	}
	switch {
	case !utf8.ValidString(m.Subject) || strings.TrimSpace(m.Subject) == "" || utf8.RuneCountInString(m.Subject) > maxSubjectRunes:
		return invalid("subject")
	case strings.IndexFunc(m.Subject, unicode.IsControl) >= 0:
		return invalid("subject contains control characters")
	case !utf8.ValidString(m.Body) || strings.TrimSpace(m.Body) == "":
		return invalid("body")
	case strings.ContainsRune(m.Body, 0):
		return invalid("body contains NUL")
	}
	return nil
}

func safeToken(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// temporaryError marks a failure worth retrying.
type temporaryError struct{ err error }

func (e temporaryError) Error() string { return e.err.Error() }
func (e temporaryError) Unwrap() error { return e.err }

// Temporary wraps err as retryable.
func Temporary(err error) error {
	if err == nil {
		return nil
	}
	return temporaryError{err}
}

// IsTemporary reports whether delivery may succeed if retried. Everything
// not explicitly marked temporary is permanent.
func IsTemporary(err error) bool {
	var t temporaryError
	return errors.As(err, &t)
}

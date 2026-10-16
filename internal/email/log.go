package email

import (
	"context"
	"log/slog"
)

// LogMailer is the development transport. It validates and "delivers" by
// logging the kind and reference only: recipients and bodies, which may hold
// secret links, are never written. Use Mailpit to read actual messages.
type LogMailer struct {
	Logger *slog.Logger
}

func (l LogMailer) Send(ctx context.Context, message Message) error {
	if err := message.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return Temporary(err)
	}
	l.Logger.Info("email captured by the log transport (not delivered)", "kind", message.Kind, "ref", message.Ref, "recipient", "<redacted>", "bodyBytes", len(message.Body))
	return nil
}

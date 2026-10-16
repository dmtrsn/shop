package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sweetshop/internal/domain"
	"sweetshop/internal/email"
)

const emailUsage = `Usage:
  shopctl email test --to ADDRESS [--timeout=30s]

Sends one harmless test message through the configured SMTP transport
(EMAIL_TRANSPORT=smtp, SMTP_*, PUBLIC_SHOP_ORIGIN, APP_MODE), with the same
TLS rules as the server. It needs no database. A delivered test message proves
the SMTP settings work, not that mail will avoid spam folders: the sender
domain still needs SPF, DKIM and DMARC.
`

func emailCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "test" {
		if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
			_, _ = io.WriteString(stdout, emailUsage)
			return exitOK
		}
		_, _ = io.WriteString(stderr, emailUsage)
		return exitUsage
	}
	flags := flag.NewFlagSet("email test", flag.ContinueOnError)
	flags.SetOutput(stderr)
	to := flags.String("to", "", "recipient address")
	timeout := flags.Duration("timeout", 30*time.Second, "limit for the delivery")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "shopctl email test: unexpected argument %q\n", flags.Arg(0))
		return exitUsage
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "shopctl email test: --timeout must be positive")
		return exitUsage
	}
	recipient, ok := domain.NormalizeEmail(*to)
	if !ok {
		fmt.Fprintln(stderr, "shopctl email test: --to must be a valid email address")
		return exitUsage
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	mode := os.Getenv("APP_MODE")
	if mode == "" {
		mode = "production"
	}
	if mode != "production" && mode != "development" && mode != "demo" {
		logger.Error("APP_MODE must be production, development, or demo")
		return exitUsage
	}
	config, err := email.LoadConfig(os.Getenv, mode == "production")
	if err != nil {
		logger.Error("invalid email configuration", "error", err)
		return exitUsage
	}
	if config.Transport != email.TransportSMTP {
		logger.Error("EMAIL_TRANSPORT=smtp is required to send a test message")
		return exitUsage
	}
	mailer, err := config.NewMailer(logger)
	if err != nil {
		logger.Error("invalid email configuration", "error", err)
		return exitUsage
	}
	message, err := email.TestMessage(recipient)
	if err != nil {
		logger.Error("build test message", "error", err)
		return exitFailure
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	logger.Info("sending test email", "transport", config.Describe())
	if err := mailer.Send(ctx, message); err != nil {
		logger.Error("test email failed", "temporary", email.IsTemporary(err), "error", err)
		return exitFailure
	}
	logger.Info("test email accepted by the SMTP server")
	if err := json.NewEncoder(stdout).Encode(map[string]any{"action": "email-test", "sent": true}); err != nil {
		return exitFailure
	}
	return exitOK
}

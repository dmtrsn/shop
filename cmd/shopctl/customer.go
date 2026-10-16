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
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	postgresrepo "sweetshop/internal/repository/postgres"
)

const customerUsage = `Usage:
  shopctl customer create --email E --name N [--phone P] --password-stdin
  shopctl customer set-password --email E --password-stdin
  shopctl customer disable --email E
  shopctl customer enable --email E

The password is one line on standard input, never an argument or environment
variable. A terminal is refused because it would echo the password:
  read -rs PASSWORD && printf '%s\n' "$PASSWORD" | shopctl customer set-password --email E --password-stdin; unset PASSWORD

set-password, disable and enable all revoke every session of the account. The commands first verify the schema like the server does, so they refuse
a database that is not migrated or, in production, one with demo fixtures.
`

const (
	customerTimeout  = time.Minute
	maxPasswordInput = 4096
)

func customer(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, customerUsage)
		return exitUsage
	}
	command := args[0]
	flags := flag.NewFlagSet("customer "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	email := flags.String("email", "", "account email")
	var name, phone *string
	var passwordStdin *bool
	switch command {
	case "create":
		name = flags.String("name", "", "customer name")
		phone = flags.String("phone", "", "optional phone number")
		passwordStdin = flags.Bool("password-stdin", false, "read the password from standard input (required)")
	case "set-password":
		passwordStdin = flags.Bool("password-stdin", false, "read the password from standard input (required)")
	case "disable", "enable":
	case "help", "-h", "--help":
		_, _ = io.WriteString(stdout, customerUsage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "shopctl customer: unknown command %q\n\n%s", command, customerUsage)
		return exitUsage
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "shopctl customer %s: unexpected argument %q\n", command, flags.Arg(0))
		return exitUsage
	}
	if strings.TrimSpace(*email) == "" {
		fmt.Fprintf(stderr, "shopctl customer %s: --email is required\n", command)
		return exitUsage
	}
	password := ""
	if passwordStdin != nil {
		if !*passwordStdin {
			fmt.Fprintf(stderr, "shopctl customer %s: --password-stdin is required\n", command)
			return exitUsage
		}
		var err error
		if password, err = readPassword(stdin); err != nil {
			fmt.Fprintf(stderr, "shopctl customer %s: %v\n", command, err)
			return exitUsage
		}
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	env, err := loadEnvironment()
	if err != nil {
		logger.Error(err.Error())
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, customerTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, env.config)
	if err != nil {
		logger.Error("connect postgres", "error", err)
		return exitFailure
	}
	defer pool.Close()
	if err := env.migrator(logger).Verify(ctx, pool); err != nil {
		if errors.Is(err, postgresrepo.ErrSchemaNotCurrent) {
			logger.Error("schema is not current; run `shopctl migrate` first", "error", err)
		} else {
			logger.Error("schema verification failed", "error", err)
		}
		return exitFailure
	}

	accounts := application.NewCustomerAccounts(postgresrepo.New(pool))
	result := map[string]any{"action": command}
	switch command {
	case "create":
		var account domain.CustomerAccount
		account, err = accounts.Create(ctx, application.NewCustomerInput{Email: *email, Name: *name, Phone: *phone, Password: password})
		result["account"] = account
	default:
		var change domain.AccountChange
		switch command {
		case "set-password":
			change, err = accounts.SetPassword(ctx, *email, password)
		case "disable":
			change, err = accounts.Disable(ctx, *email)
		case "enable":
			change, err = accounts.Enable(ctx, *email)
		}
		result["account"], result["revokedSessions"] = change.Account, change.RevokedSessions
	}
	if code := customerError(stderr, logger, command, err); code != exitOK {
		return code
	}
	account := result["account"].(domain.CustomerAccount)
	logger.Info("customer "+command, "id", account.ID, "mode", env.mode)
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return exitFailure
	}
	return exitOK
}

func customerError(stderr io.Writer, logger *slog.Logger, command string, err error) int {
	var validation *domain.ValidationError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &validation):
		for _, field := range validation.Fields {
			fmt.Fprintf(stderr, "shopctl customer %s: %s: %s\n", command, field.Field, field.Message)
		}
		return exitUsage
	case errors.Is(err, domain.ErrEmailTaken):
		fmt.Fprintf(stderr, "shopctl customer %s: an account with this email already exists\n", command)
		return exitConflict
	case errors.Is(err, domain.ErrNotFound):
		fmt.Fprintf(stderr, "shopctl customer %s: no customer account has this email\n", command)
		return exitConflict
	default:
		logger.Error("customer "+command+" failed", "error", err)
		return exitFailure
	}
}

// readPassword reads exactly one line from stdin. It refuses a terminal,
// which would echo the password, and strips only the line ending.
func readPassword(stdin io.Reader) (string, error) {
	if file, ok := stdin.(*os.File); ok {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return "", errors.New("standard input is a terminal; pipe the password instead")
		}
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxPasswordInput+1))
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if len(data) > maxPasswordInput {
		return "", errors.New("standard input is too long for a password")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if strings.ContainsAny(password, "\r\n") {
		return "", errors.New("standard input must contain exactly one line")
	}
	if password == "" {
		return "", errors.New("standard input contains no password")
	}
	return password, nil
}

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

// Customer and staff accounts share the command shape, password input and
// output conventions, but each kind calls only its own service and tables.

const customerUsage = `Usage:
  shopctl customer create --email E --name N [--phone P] --password-stdin
  shopctl customer set-password --email E --password-stdin
  shopctl customer disable --email E
  shopctl customer enable --email E

The password is one line on standard input, never an argument or environment
variable. A terminal is refused because it would echo the password:
  read -rs PASSWORD && printf '%s\n' "$PASSWORD" | shopctl customer set-password --email E --password-stdin; unset PASSWORD

set-password, disable and enable all revoke every session of the account.
The commands first verify the schema like the server does, so they refuse a
database that is not migrated or, in production, one with demo fixtures.
`

const staffUsage = `Usage:
  shopctl staff create --email E --name DISPLAY_NAME --password-stdin
  shopctl staff set-password --email E --password-stdin
  shopctl staff disable --email E
  shopctl staff enable --email E

Staff accounts are separate from customer accounts: their own table,
sessions and cookie. Passwords need at least 12 characters and are read from
standard input exactly like customer passwords:
  read -rs PASSWORD && printf '%s\n' "$PASSWORD" | shopctl staff set-password --email E --password-stdin; unset PASSWORD

set-password, disable and enable all revoke every staff session of the
account. The schema is verified first, as for customer commands.
`

const (
	accountTimeout   = time.Minute
	maxPasswordInput = 4096
)

type accountInput struct {
	email, name, phone, password string
}

type accountKind struct {
	name  string
	usage string
	phone bool
	// apply runs command against the kind's own service and returns the
	// account for the output and log.
	apply func(ctx context.Context, pool *pgxpool.Pool, command string, input accountInput) (account any, id string, revoked *int64, err error)
}

var customerKind = accountKind{name: "customer", usage: customerUsage, phone: true, apply: applyCustomer}

var staffKind = accountKind{name: "staff", usage: staffUsage, apply: applyStaff}

func customer(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return accountCommand(customerKind, args, stdin, stdout, stderr)
}

func staff(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return accountCommand(staffKind, args, stdin, stdout, stderr)
}

func accountCommand(kind accountKind, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, kind.usage)
		return exitUsage
	}
	command := args[0]
	prefix := "shopctl " + kind.name + " " + command
	flags := flag.NewFlagSet(kind.name+" "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	email := flags.String("email", "", "account email")
	name, phone := new(string), new(string)
	var passwordStdin *bool
	switch command {
	case "create":
		name = flags.String("name", "", kind.name+" name")
		if kind.phone {
			phone = flags.String("phone", "", "optional phone number")
		}
		passwordStdin = flags.Bool("password-stdin", false, "read the password from standard input (required)")
	case "set-password":
		passwordStdin = flags.Bool("password-stdin", false, "read the password from standard input (required)")
	case "disable", "enable":
	case "help", "-h", "--help":
		_, _ = io.WriteString(stdout, kind.usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "shopctl %s: unknown command %q\n\n", kind.name, command)
		_, _ = io.WriteString(stderr, kind.usage)
		return exitUsage
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", prefix, flags.Arg(0))
		return exitUsage
	}
	if strings.TrimSpace(*email) == "" {
		fmt.Fprintf(stderr, "%s: --email is required\n", prefix)
		return exitUsage
	}
	input := accountInput{email: *email, name: *name, phone: *phone}
	if passwordStdin != nil {
		if !*passwordStdin {
			fmt.Fprintf(stderr, "%s: --password-stdin is required\n", prefix)
			return exitUsage
		}
		var err error
		if input.password, err = readPassword(stdin); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", prefix, err)
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
	ctx, cancel := context.WithTimeout(ctx, accountTimeout)
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

	account, id, revoked, err := kind.apply(ctx, pool, command, input)
	if code := accountError(stderr, logger, prefix, err); code != exitOK {
		return code
	}
	logger.Info(kind.name+" "+command, "id", id, "mode", env.mode)
	result := map[string]any{"action": command, "account": account}
	if revoked != nil {
		result["revokedSessions"] = *revoked
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return exitFailure
	}
	return exitOK
}

func applyCustomer(ctx context.Context, pool *pgxpool.Pool, command string, input accountInput) (any, string, *int64, error) {
	accounts := application.NewCustomerAccounts(postgresrepo.New(pool))
	if command == "create" {
		account, err := accounts.Create(ctx, application.NewCustomerInput{Email: input.email, Name: input.name, Phone: input.phone, Password: input.password})
		return account, account.ID, nil, err
	}
	var change domain.AccountChange
	var err error
	switch command {
	case "set-password":
		change, err = accounts.SetPassword(ctx, input.email, input.password)
	case "disable":
		change, err = accounts.Disable(ctx, input.email)
	case "enable":
		change, err = accounts.Enable(ctx, input.email)
	}
	return change.Account, change.Account.ID, &change.RevokedSessions, err
}

func applyStaff(ctx context.Context, pool *pgxpool.Pool, command string, input accountInput) (any, string, *int64, error) {
	accounts := application.NewStaffAccounts(postgresrepo.New(pool))
	if command == "create" {
		account, err := accounts.Create(ctx, application.NewStaffInput{Email: input.email, DisplayName: input.name, Password: input.password})
		return account, account.ID, nil, err
	}
	var change domain.StaffChange
	var err error
	switch command {
	case "set-password":
		change, err = accounts.SetPassword(ctx, input.email, input.password)
	case "disable":
		change, err = accounts.Disable(ctx, input.email)
	case "enable":
		change, err = accounts.Enable(ctx, input.email)
	}
	return change.Account, change.Account.ID, &change.RevokedSessions, err
}

func accountError(stderr io.Writer, logger *slog.Logger, prefix string, err error) int {
	var validation *domain.ValidationError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &validation):
		for _, field := range validation.Fields {
			fmt.Fprintf(stderr, "%s: %s: %s\n", prefix, field.Field, field.Message)
		}
		return exitUsage
	case errors.Is(err, domain.ErrEmailTaken):
		fmt.Fprintf(stderr, "%s: an account with this email already exists\n", prefix)
		return exitConflict
	case errors.Is(err, domain.ErrNotFound):
		fmt.Fprintf(stderr, "%s: no account has this email\n", prefix)
		return exitConflict
	default:
		logger.Error(prefix+" failed", "error", err)
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

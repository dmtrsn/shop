package email

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"sweetshop/internal/domain"
)

// Transports.
const (
	TransportDisabled = ""
	TransportSMTP     = "smtp"
	// TransportLog records only the kind and reference of each message; it
	// is refused in production.
	TransportLog = "log"
)

// Config is the email configuration of one process.
type Config struct {
	Transport string
	SMTP      SMTPConfig
	// PublicOrigin is required whenever a transport is configured, because
	// emailed links use it.
	PublicOrigin PublicOrigin
}

func (c Config) Enabled() bool { return c.Transport != TransportDisabled }

// LoadConfig reads EMAIL_TRANSPORT, SMTP_HOST, SMTP_PORT, SMTP_TLS,
// SMTP_USERNAME, SMTP_PASSWORD, SMTP_FROM and PUBLIC_SHOP_ORIGIN. Production
// requires SMTP over TLS with credentials and an https origin; the log
// transport and plaintext SMTP are development-only. Errors name variables,
// never values.
func LoadConfig(getenv func(string) string, production bool) (Config, error) {
	config := Config{Transport: getenv("EMAIL_TRANSPORT")}
	if raw := getenv("PUBLIC_SHOP_ORIGIN"); raw != "" {
		origin, err := ParsePublicOrigin(raw, production)
		if err != nil {
			return Config{}, err
		}
		config.PublicOrigin = origin
	}
	smtpSet := false
	for _, name := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_TLS", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM"} {
		smtpSet = smtpSet || getenv(name) != ""
	}
	switch config.Transport {
	case TransportDisabled:
		if smtpSet {
			return Config{}, errors.New("SMTP_* is set but EMAIL_TRANSPORT is empty; set EMAIL_TRANSPORT=smtp or remove the SMTP settings")
		}
		return config, nil
	case TransportLog:
		if production {
			return Config{}, errors.New("EMAIL_TRANSPORT=log is not allowed in production")
		}
		if smtpSet {
			return Config{}, errors.New("SMTP_* is set but EMAIL_TRANSPORT=log")
		}
	case TransportSMTP:
		smtp, err := loadSMTP(getenv, production)
		if err != nil {
			return Config{}, err
		}
		config.SMTP = smtp
	default:
		return Config{}, errors.New("EMAIL_TRANSPORT must be smtp, log or empty")
	}
	if config.PublicOrigin == (PublicOrigin{}) {
		return Config{}, errors.New("PUBLIC_SHOP_ORIGIN is required when EMAIL_TRANSPORT is set")
	}
	return config, nil
}

func loadSMTP(getenv func(string) string, production bool) (SMTPConfig, error) {
	config := SMTPConfig{
		Host:     getenv("SMTP_HOST"),
		TLS:      TLSMode(getenv("SMTP_TLS")),
		Username: getenv("SMTP_USERNAME"),
		Password: getenv("SMTP_PASSWORD"),
		From:     getenv("SMTP_FROM"),
	}
	switch config.TLS {
	case TLSImplicit:
		config.Port = 465
	case TLSStartTLS:
		config.Port = 587
	case TLSNone:
		if production {
			return SMTPConfig{}, errors.New("SMTP_TLS=none is not allowed in production; use implicit or starttls")
		}
		config.Port = 25
	default:
		return SMTPConfig{}, errors.New("SMTP_TLS must be implicit, starttls or none")
	}
	if raw := getenv("SMTP_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return SMTPConfig{}, errors.New("SMTP_PORT must be between 1 and 65535")
		}
		config.Port = port
	}
	if production && config.Username == "" {
		return SMTPConfig{}, errors.New("SMTP_USERNAME and SMTP_PASSWORD are required in production")
	}
	if from, ok := normalizedAddress(config.From); !ok || from != config.From {
		return SMTPConfig{}, errors.New("SMTP_FROM must be a normalized email address")
	}
	if _, err := NewSMTPMailer(config); err != nil {
		return SMTPConfig{}, fmt.Errorf("SMTP configuration: %w", err)
	}
	return config, nil
}

// NewMailer returns the configured transport, or nil when email is disabled.
func (c Config) NewMailer(logger *slog.Logger) (Mailer, error) {
	switch c.Transport {
	case TransportSMTP:
		return NewSMTPMailer(c.SMTP)
	case TransportLog:
		return LogMailer{Logger: logger}, nil
	default:
		return nil, nil
	}
}

// Describe is a startup-log summary without credentials or addresses.
func (c Config) Describe() string {
	switch c.Transport {
	case TransportSMTP:
		return fmt.Sprintf("smtp tls=%s port=%d auth=%t", c.SMTP.TLS, c.SMTP.Port, c.SMTP.Username != "")
	case TransportLog:
		return "log (development only; messages are not delivered)"
	default:
		return "disabled"
	}
}

func normalizedAddress(raw string) (string, bool) {
	if strings.ContainsAny(raw, "\r\n<>") {
		return "", false
	}
	return domain.NormalizeEmail(raw)
}

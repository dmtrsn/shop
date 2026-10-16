package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// TLSMode is how the SMTP connection is protected.
type TLSMode string

const (
	// TLSImplicit connects with TLS from the first byte (SMTPS, usually 465).
	TLSImplicit TLSMode = "implicit"
	// TLSStartTLS requires the server to offer STARTTLS and upgrades before
	// anything else is sent (submission, usually 587). There is no fallback.
	TLSStartTLS TLSMode = "starttls"
	// TLSNone is plaintext, allowed only outside production (for example a
	// local Mailpit) and never with credentials.
	TLSNone TLSMode = "none"
)

const defaultSendTimeout = 20 * time.Second

// SMTPConfig is validated by NewSMTPMailer.
type SMTPConfig struct {
	Host     string
	Port     int
	TLS      TLSMode
	Username string
	Password string
	From     string
	// RootCAs replaces the system roots. Tests only; it is not configurable
	// from the environment, and verification is never disabled.
	RootCAs *x509.CertPool
	// Timeout bounds one delivery; zero means 20 s.
	Timeout time.Duration
}

// SMTPMailer delivers over SMTP. One connection per message.
type SMTPMailer struct {
	config SMTPConfig
}

// NewSMTPMailer validates the configuration. Errors never contain the
// credentials or other values.
func NewSMTPMailer(config SMTPConfig) (*SMTPMailer, error) {
	switch {
	case !validHost(config.Host):
		return nil, errors.New("SMTP host is invalid")
	case config.Port < 1 || config.Port > 65535:
		return nil, errors.New("SMTP port is out of range")
	case config.TLS != TLSImplicit && config.TLS != TLSStartTLS && config.TLS != TLSNone:
		return nil, errors.New("SMTP TLS mode must be implicit, starttls or none")
	case (config.Username == "") != (config.Password == ""):
		return nil, errors.New("SMTP username and password must be set together")
	case config.TLS == TLSNone && config.Username != "":
		return nil, errors.New("SMTP credentials are never sent without TLS")
	}
	if from, ok := normalizedAddress(config.From); !ok || from != config.From {
		return nil, errors.New("SMTP from address is invalid")
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultSendTimeout
	}
	return &SMTPMailer{config: config}, nil
}

// SMTPError is an SMTP protocol failure. It keeps only the stage and the
// reply code: server text can echo recipients or credentials.
type SMTPError struct {
	Stage string
	Code  int
}

func (e *SMTPError) Error() string { return fmt.Sprintf("smtp %s: status %d", e.Stage, e.Code) }

// stageError wraps a transport failure (dial, TLS) with its stage.
type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return "smtp " + e.stage + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func (s *SMTPMailer) Send(ctx context.Context, message Message) (err error) {
	if err := message.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	conn, err := s.dial(ctx)
	if err != nil {
		return err
	}
	// Cancellation closes the connection, which unblocks any SMTP command.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		_ = conn.Close()
		return classify(ctx, "greeting", err)
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return classify(ctx, "ehlo", err)
	}
	if s.config.TLS == TLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return &stageError{stage: "starttls", err: errors.New("server does not offer STARTTLS; refusing to continue without TLS")}
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			return classify(ctx, "starttls", err)
		}
	}
	if s.config.Username != "" {
		// Independent of net/smtp's localhost exemption: never authenticate
		// on a connection that is not TLS-protected and verified.
		if state, ok := client.TLSConnectionState(); !ok || !state.HandshakeComplete || len(state.VerifiedChains) == 0 {
			return &stageError{stage: "auth", err: errors.New("refusing to authenticate without a verified TLS connection")}
		}
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return classify(ctx, "auth", err)
		}
	}
	if err := client.Mail(s.config.From); err != nil {
		return classify(ctx, "mail", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return classify(ctx, "rcpt", err)
	}
	writer, err := client.Data()
	if err != nil {
		return classify(ctx, "data", err)
	}
	if _, err := writer.Write(s.compose(message)); err != nil {
		return classify(ctx, "data", err)
	}
	if err := writer.Close(); err != nil {
		return classify(ctx, "data", err)
	}
	// The message is accepted; a failed QUIT changes nothing.
	_ = client.Quit()
	return nil
}

func (s *SMTPMailer) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12, RootCAs: s.config.RootCAs}
}

func (s *SMTPMailer) dial(ctx context.Context) (net.Conn, error) {
	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, Temporary(&stageError{stage: "dial", err: err})
	}
	if s.config.TLS != TLSImplicit {
		return conn, nil
	}
	tlsConn := tls.Client(conn, s.tlsConfig())
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, classify(ctx, "tls", err)
	}
	return tlsConn, nil
}

// compose builds RFC 5322 headers from validated fields and a
// quoted-printable UTF-8 body. The data writer applies dot-stuffing.
func (s *SMTPMailer) compose(message Message) []byte {
	var out bytes.Buffer
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	domain := s.config.From[strings.LastIndexByte(s.config.From, '@')+1:]
	header := func(name, value string) { out.WriteString(name + ": " + value + "\r\n") }
	header("From", "<"+s.config.From+">")
	header("To", "<"+message.To+">")
	header("Subject", mime.QEncoding.Encode("utf-8", message.Subject))
	header("Date", time.Now().UTC().Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	header("Auto-Submitted", "auto-generated")
	out.WriteString("\r\n")
	body := quotedprintable.NewWriter(&out)
	_, _ = body.Write([]byte(strings.ReplaceAll(message.Body, "\r\n", "\n")))
	_ = body.Close()
	return out.Bytes()
}

// classify keeps temporary failures retryable: cancellation, timeouts,
// dropped connections and 4xx replies. TLS verification, 5xx replies and
// anything unknown are permanent.
func classify(ctx context.Context, stage string, err error) error {
	var protocol *textproto.Error
	switch {
	case errors.As(err, &protocol):
		smtpErr := &SMTPError{Stage: stage, Code: protocol.Code}
		if protocol.Code >= 400 && protocol.Code < 500 {
			return Temporary(smtpErr)
		}
		return smtpErr
	case isTLSFailure(err):
		return &stageError{stage: stage, err: err}
	case ctx.Err() != nil:
		return Temporary(&stageError{stage: stage, err: ctx.Err()})
	case isNetworkFailure(err):
		return Temporary(&stageError{stage: stage, err: err})
	default:
		return &stageError{stage: stage, err: err}
	}
}

func isTLSFailure(err error) bool {
	var (
		verification *tls.CertificateVerificationError
		unknown      x509.UnknownAuthorityError
		hostname     x509.HostnameError
		invalid      x509.CertificateInvalidError
		record       tls.RecordHeaderError
		alert        tls.AlertError
	)
	return errors.As(err, &verification) || errors.As(err, &unknown) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &record) || errors.As(err, &alert)
}

func isNetworkFailure(err error) bool {
	var netErr net.Error
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) ||
		(errors.As(err, &netErr) && netErr.Timeout())
}

// validHost accepts a DNS name or an IP address, without a port.
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(strings.ToLower(host), ".") {
		if len(label) > 63 || !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

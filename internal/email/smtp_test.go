package email_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"sweetshop/internal/email"
)

const smtpPassword = "SENTINEL-SMTP-PASSWORD-9f2c"

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newAuthority(t *testing.T) authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return authority{cert, key, pool}
}

// leaf issues a server certificate for the given DNS names and IPs.
func (a authority) leaf(t *testing.T, dns []string, ips []net.IP) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "smtp"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), DNSNames: dns, IPAddresses: ips, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeSMTP is a minimal SMTP server that records each command with whether
// TLS was active when it arrived.
type fakeSMTP struct {
	cert      *tls.Certificate
	implicit  bool
	startTLS  bool
	rcptReply string
	authReply string
	silent    bool

	ln     net.Listener
	mu     sync.Mutex
	events []string
	data   []string
	auths  []string
}

func (f *fakeSMTP) start(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func (f *fakeSMTP) record(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeSMTP) snapshot() (events, data, auths []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...), append([]string(nil), f.data...), append([]string(nil), f.auths...)
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	if f.silent {
		time.Sleep(2 * time.Second)
		return
	}
	config := &tls.Config{}
	if f.cert != nil {
		config.Certificates = []tls.Certificate{*f.cert}
	}
	tlsOn := false
	if f.implicit {
		server := tls.Server(conn, config)
		if err := server.Handshake(); err != nil {
			f.record("TLS-FAILED")
			return
		}
		conn, tlsOn = server, true
	}
	text := textproto.NewConn(conn)
	_ = text.PrintfLine("220 fake ESMTP")
	for {
		line, err := text.ReadLine()
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		verb := strings.ToUpper(fields[0])
		f.record(verb + " tls=" + strconv.FormatBool(tlsOn))
		switch verb {
		case "EHLO", "HELO":
			lines := []string{"250-fake"}
			if f.startTLS && !tlsOn {
				lines = append(lines, "250-STARTTLS")
			}
			lines = append(lines, "250-AUTH PLAIN", "250 8BITMIME")
			for _, l := range lines {
				_ = text.PrintfLine("%s", l)
			}
		case "STARTTLS":
			_ = text.PrintfLine("220 go ahead")
			server := tls.Server(conn, config)
			if err := server.Handshake(); err != nil {
				f.record("TLS-FAILED")
				return
			}
			conn, tlsOn = server, true
			text = textproto.NewConn(conn)
		case "AUTH":
			if len(fields) > 2 {
				decoded, _ := base64.StdEncoding.DecodeString(fields[2])
				f.mu.Lock()
				f.auths = append(f.auths, string(decoded))
				f.mu.Unlock()
			}
			reply := f.authReply
			if reply == "" {
				reply = "235 2.7.0 accepted"
			}
			_ = text.PrintfLine("%s", reply)
		case "MAIL":
			_ = text.PrintfLine("250 ok")
		case "RCPT":
			reply := f.rcptReply
			if reply == "" {
				reply = "250 ok"
			}
			_ = text.PrintfLine("%s", reply)
		case "DATA":
			_ = text.PrintfLine("354 go ahead")
			lines, err := text.ReadDotLines()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.data = append(f.data, strings.Join(lines, "\n"))
			f.mu.Unlock()
			_ = text.PrintfLine("250 queued")
		case "QUIT":
			_ = text.PrintfLine("221 bye")
			return
		default:
			_ = text.PrintfLine("502 unknown")
		}
	}
}

func mailer(t *testing.T, config email.SMTPConfig) *email.SMTPMailer {
	t.Helper()
	if config.Host == "" {
		config.Host = "127.0.0.1"
	}
	if config.From == "" {
		config.From = "shop@example.com"
	}
	if config.Timeout == 0 {
		config.Timeout = 3 * time.Second
	}
	m, err := email.NewSMTPMailer(config)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// decode splits a received message into headers and the decoded body.
func decode(t *testing.T, raw string) (map[string]string, string) {
	t.Helper()
	head, body, ok := strings.Cut(raw, "\n\n")
	if !ok {
		t.Fatalf("no header/body separator in %q", raw)
	}
	headers := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		name, value, _ := strings.Cut(line, ": ")
		headers[name] = value
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	return headers, strings.ReplaceAll(string(decoded), "\r\n", "\n")
}

var ip = []net.IP{net.ParseIP("127.0.0.1")}

func TestSMTPStartTLSAuthenticatesOnlyAfterTLS(t *testing.T) {
	ca := newAuthority(t)
	cert := ca.leaf(t, nil, ip)
	server := &fakeSMTP{cert: &cert, startTLS: true}
	port := server.start(t)
	m := mailer(t, email.SMTPConfig{Port: port, TLS: email.TLSStartTLS, Username: "shop-user", Password: smtpPassword, RootCAs: ca.pool})
	message := secretMessage(t, "usr_1")
	message.Body += "\n.строка с точкой в начале\n"
	if err := m.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	events, data, auths := server.snapshot()
	want := []string{"EHLO tls=false", "STARTTLS tls=false", "EHLO tls=true", "AUTH tls=true", "MAIL tls=true", "RCPT tls=true", "DATA tls=true", "QUIT tls=true"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events %v", events)
	}
	if len(auths) != 1 || auths[0] != "\x00shop-user\x00"+smtpPassword {
		t.Fatalf("auth %q", auths)
	}
	headers, body := decode(t, data[0])
	subject, err := new(mime.WordDecoder).DecodeHeader(headers["Subject"])
	if err != nil || subject != message.Subject {
		t.Fatalf("subject %q %v", subject, err)
	}
	if headers["From"] != "<shop@example.com>" || headers["To"] != "<ann@example.com>" || headers["Content-Type"] != "text/plain; charset=utf-8" ||
		headers["Content-Transfer-Encoding"] != "quoted-printable" || headers["Auto-Submitted"] != "auto-generated" || !strings.HasSuffix(headers["Message-ID"], "@example.com>") {
		t.Fatalf("headers %v", headers)
	}
	// SMTP DATA ends every body with CRLF before the closing ".", so the
	// trailing line break is not distinguishable on the server side.
	if strings.TrimRight(body, "\n") != strings.TrimRight(message.Body, "\n") || !strings.Contains(body, "\n.строка с точкой") {
		t.Fatalf("body round trip:\n%q\n%q", body, message.Body)
	}
}

func TestSMTPImplicitTLS(t *testing.T) {
	ca := newAuthority(t)
	cert := ca.leaf(t, nil, ip)
	server := &fakeSMTP{cert: &cert, implicit: true}
	port := server.start(t)
	if err := mailer(t, email.SMTPConfig{Port: port, TLS: email.TLSImplicit, Username: "u", Password: smtpPassword, RootCAs: ca.pool}).Send(context.Background(), secretMessage(t, "r")); err != nil {
		t.Fatal(err)
	}
	events, data, _ := server.snapshot()
	if len(data) != 1 || events[0] != "EHLO tls=true" {
		t.Fatalf("events %v", events)
	}
}

func TestSMTPRefusesWhatTLSCannotProtect(t *testing.T) {
	ca := newAuthority(t)
	other := newAuthority(t)
	trusted := ca.leaf(t, nil, ip)
	wrongName := ca.leaf(t, []string{"mail.example"}, nil)
	for name, tc := range map[string]struct {
		server *fakeSMTP
		mode   email.TLSMode
		roots  *x509.CertPool
	}{
		"untrusted certificate (STARTTLS)":  {&fakeSMTP{cert: &trusted, startTLS: true}, email.TLSStartTLS, other.pool},
		"untrusted certificate (implicit)":  {&fakeSMTP{cert: &trusted, implicit: true}, email.TLSImplicit, other.pool},
		"hostname mismatch":                 {&fakeSMTP{cert: &wrongName, implicit: true}, email.TLSImplicit, ca.pool},
		"system roots do not trust test CA": {&fakeSMTP{cert: &trusted, startTLS: true}, email.TLSStartTLS, nil},
		"STARTTLS not offered":              {&fakeSMTP{cert: &trusted, startTLS: false}, email.TLSStartTLS, ca.pool},
	} {
		t.Run(name, func(t *testing.T) {
			port := tc.server.start(t)
			err := mailer(t, email.SMTPConfig{Port: port, TLS: tc.mode, Username: "shop-user", Password: smtpPassword, RootCAs: tc.roots}).Send(context.Background(), secretMessage(t, "r"))
			if err == nil || email.IsTemporary(err) {
				t.Fatalf("expected a permanent failure, got %v", err)
			}
			events, data, auths := tc.server.snapshot()
			for _, event := range events {
				if strings.HasPrefix(event, "AUTH") || strings.HasPrefix(event, "MAIL") {
					t.Fatalf("%s sent without verified TLS: %v", event, events)
				}
			}
			if len(data) != 0 || len(auths) != 0 {
				t.Fatal("message or credentials delivered")
			}
			for _, secret := range []string{smtpPassword, "shop-user", secretToken, "ann@example.com"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks %q: %v", secret, err)
				}
			}
		})
	}
}

func TestSMTPClassifiesReplies(t *testing.T) {
	ca := newAuthority(t)
	cert := ca.leaf(t, nil, ip)
	for name, tc := range map[string]struct {
		server    *fakeSMTP
		temporary bool
		stage     string
		code      int
	}{
		"4xx recipient is temporary":    {&fakeSMTP{cert: &cert, implicit: true, rcptReply: "451 4.3.0 try later"}, true, "rcpt", 451},
		"5xx recipient is permanent":    {&fakeSMTP{cert: &cert, implicit: true, rcptReply: "550 5.1.1 <ann@example.com> unknown " + secretToken}, false, "rcpt", 550},
		"bad credentials are permanent": {&fakeSMTP{cert: &cert, implicit: true, authReply: "535 5.7.8 bad credentials for shop-user " + smtpPassword}, false, "auth", 535},
	} {
		t.Run(name, func(t *testing.T) {
			port := tc.server.start(t)
			err := mailer(t, email.SMTPConfig{Port: port, TLS: email.TLSImplicit, Username: "shop-user", Password: smtpPassword, RootCAs: ca.pool}).Send(context.Background(), secretMessage(t, "r"))
			var smtpErr *email.SMTPError
			if !errors.As(err, &smtpErr) || smtpErr.Stage != tc.stage || smtpErr.Code != tc.code || email.IsTemporary(err) != tc.temporary {
				t.Fatalf("%v", err)
			}
			// Server text is dropped: it can echo recipients or credentials.
			for _, secret := range []string{smtpPassword, "shop-user", secretToken, "ann@example.com", "unknown"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error keeps server text %q: %v", secret, err)
				}
			}
		})
	}
}

func TestSMTPNetworkFailuresAreTemporary(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	err = mailer(t, email.SMTPConfig{Port: closedPort, TLS: email.TLSNone}).Send(context.Background(), secretMessage(t, "r"))
	if err == nil || !email.IsTemporary(err) {
		t.Fatalf("connection refused: %v", err)
	}
	silent := &fakeSMTP{silent: true}
	port := silent.start(t)
	started := time.Now()
	err = mailer(t, email.SMTPConfig{Port: port, TLS: email.TLSNone, Timeout: 200 * time.Millisecond}).Send(context.Background(), secretMessage(t, "r"))
	if err == nil || !email.IsTemporary(err) || time.Since(started) > time.Second {
		t.Fatalf("silent server: %v after %v", err, time.Since(started))
	}
}

func TestSMTPPlaintextOnlyWithoutCredentials(t *testing.T) {
	server := &fakeSMTP{}
	port := server.start(t)
	if err := mailer(t, email.SMTPConfig{Port: port, TLS: email.TLSNone}).Send(context.Background(), secretMessage(t, "r")); err != nil {
		t.Fatal(err)
	}
	if _, data, _ := server.snapshot(); len(data) != 1 {
		t.Fatal("not delivered")
	}
	for name, config := range map[string]email.SMTPConfig{
		"credentials over plaintext": {Host: "127.0.0.1", Port: 25, TLS: email.TLSNone, Username: "u", Password: smtpPassword, From: "shop@example.com"},
		"half credentials":           {Host: "127.0.0.1", Port: 25, TLS: email.TLSImplicit, Username: "u", From: "shop@example.com"},
		"unknown mode":               {Host: "127.0.0.1", Port: 25, TLS: "opportunistic", From: "shop@example.com"},
		"host with port":             {Host: "127.0.0.1:25", Port: 25, TLS: email.TLSImplicit, From: "shop@example.com"},
		"bad from":                   {Host: "127.0.0.1", Port: 25, TLS: email.TLSImplicit, From: "shop@example.com\r\nBcc: x@evil.example"},
	} {
		if _, err := email.NewSMTPMailer(config); err == nil || strings.Contains(err.Error(), smtpPassword) || strings.Contains(err.Error(), "evil") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

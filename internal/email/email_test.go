package email_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"sweetshop/internal/email"
)

const secretToken = "SENTINEL-TOKEN-must-never-be-logged-1234567890abcdef"

func origin(t *testing.T) email.PublicOrigin {
	t.Helper()
	o, err := email.ParsePublicOrigin("https://shop.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func secretLink(t *testing.T) email.Link {
	t.Helper()
	link, err := origin(t).Link("/register/confirm", "t="+secretToken)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

func TestMessageValidation(t *testing.T) {
	valid := email.Message{Kind: email.KindTest, Ref: "usr_1", To: "ann@example.com", Subject: "Тема письма", Body: "Текст\nс переводом строки"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*email.Message){
		"CRLF in subject":       func(m *email.Message) { m.Subject = "Hi\r\nBcc: x@evil.example" },
		"LF in subject":         func(m *email.Message) { m.Subject = "Hi\nBcc: x@evil.example" },
		"CRLF in recipient":     func(m *email.Message) { m.To = "ann@example.com\r\nBcc: x@evil.example" },
		"not normalized":        func(m *email.Message) { m.To = "Ann@Example.com" },
		"invalid recipient":     func(m *email.Message) { m.To = "not-an-address" },
		"two recipients":        func(m *email.Message) { m.To = "a@example.com,b@example.com" },
		"angle brackets":        func(m *email.Message) { m.To = "<ann@example.com>" },
		"invalid UTF-8 subject": func(m *email.Message) { m.Subject = "\xff\xfe" },
		"invalid UTF-8 body":    func(m *email.Message) { m.Body = "ok \xff" },
		"NUL in body":           func(m *email.Message) { m.Body = "a\x00b" },
		"empty subject":         func(m *email.Message) { m.Subject = " " },
		"empty body":            func(m *email.Message) { m.Body = "" },
		"long subject":          func(m *email.Message) { m.Subject = strings.Repeat("я", 201) },
		"unsafe kind":           func(m *email.Message) { m.Kind = "test\r\n" },
		"missing kind":          func(m *email.Message) { m.Kind = "" },
		"unsafe ref":            func(m *email.Message) { m.Ref = "usr 1\n" },
	} {
		t.Run(name, func(t *testing.T) {
			m := valid
			mutate(&m)
			err := m.Validate()
			if !errors.Is(err, email.ErrInvalidMessage) {
				t.Fatalf("accepted: %v", err)
			}
			if strings.Contains(err.Error(), "evil") || strings.Contains(err.Error(), "Bcc") {
				t.Fatal("error quotes the input")
			}
		})
	}
}

func TestMessageRedactsItselfWhenFormatted(t *testing.T) {
	m, err := email.RegistrationConfirmation("ann@example.com", "usr_1", secretLink(t), 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewTextHandler(&logs, nil)).Info("event", "message", m)
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("event", "message", m)
	formatted := fmt.Sprintf("%v %+v %#v %s", m, m, m, m) + logs.String() + fmt.Sprint([]email.Message{m})
	for _, secret := range []string{secretToken, "ann@example.com", "Подтвердите", "Здравствуйте"} {
		if strings.Contains(formatted, secret) {
			t.Fatalf("formatted message leaks %q", secret)
		}
	}
	if !strings.Contains(formatted, "registration_confirmation") || !strings.Contains(formatted, "usr_1") {
		t.Fatal("kind and ref should remain visible")
	}
}

func TestParsePublicOrigin(t *testing.T) {
	for _, tc := range []struct {
		raw, want  string
		production bool
	}{
		{"https://shop.example.com", "https://shop.example.com", true},
		{"https://Shop.Example.COM:443", "https://shop.example.com", true},
		{"https://shop.example.com/", "https://shop.example.com", true},
		{"https://shop.example.com:8443", "https://shop.example.com:8443", true},
		{"http://localhost:3000", "http://localhost:3000", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080", false},
	} {
		got, err := email.ParsePublicOrigin(tc.raw, tc.production)
		if err != nil || got.String() != tc.want {
			t.Errorf("%q: %q %v", tc.raw, got, err)
		}
	}
	for _, tc := range []struct {
		raw        string
		production bool
	}{
		{"", true}, {"http://shop.example.com", true}, {"http://localhost:3000", true},
		{"https://user@shop.example.com", true}, {"https://user:pw@shop.example.com", true},
		{"https://shop.example.com/path", true}, {"https://shop.example.com?q=1", true}, {"https://shop.example.com/?", true},
		{"https://shop.example.com#x", true}, {"https://*.example.com", true}, {"https://shop_example.com", true},
		{"https://shop.example.com:0", true}, {"https://shop.example.com:99999", true}, {"https://-shop.example.com", true},
		{"shop.example.com", true}, {"ftp://shop.example.com", true}, {" https://shop.example.com", true},
		{"https://shop.example.com\r\n", true}, {"https://shop%2eexample.com", true}, {"javascript:alert(1)", false},
	} {
		if got, err := email.ParsePublicOrigin(tc.raw, tc.production); err == nil {
			t.Errorf("%q accepted as %q", tc.raw, got)
		} else if tc.raw != "" && strings.Contains(err.Error(), strings.TrimSpace(tc.raw)) {
			t.Errorf("error quotes the value: %v", err)
		}
	}
}

func TestLinksComeOnlyFromThePublicOrigin(t *testing.T) {
	o := origin(t)
	link, err := o.Link("/reset-password", "t=abc_DEF-123")
	if err != nil || link.String() != "https://shop.example.com/reset-password#t=abc_DEF-123" {
		t.Fatalf("%q %v", link, err)
	}
	if plain, err := o.Link("/login", ""); err != nil || plain.String() != "https://shop.example.com/login" {
		t.Fatalf("%q %v", plain, err)
	}
	for _, tc := range []struct{ path, fragment string }{
		{"login", ""}, {"//evil.example/x", ""}, {"/a//b", ""}, {"/login?next=x", ""}, {"/Login", ""},
		{"/login", "t=a b"}, {"/login", "t=a&next=x"}, {"/login", "javascript:x"}, {"/login\r\n", ""}, {"/../x", ""},
	} {
		if _, err := o.Link(tc.path, tc.fragment); !errors.Is(err, email.ErrInvalidLink) {
			t.Errorf("%q #%q accepted", tc.path, tc.fragment)
		}
	}
	if _, err := (email.PublicOrigin{}).Link("/login", ""); err == nil {
		t.Fatal("link without a configured origin")
	}
}

func TestTemplatesRender(t *testing.T) {
	o := origin(t)
	signIn, _ := o.Link("/login", "")
	recovery, _ := o.Link("/forgot-password", "")
	resetLink, _ := o.Link("/reset-password", "t="+secretToken)
	confirm, err := email.RegistrationConfirmation("ann@example.com", "pending_1", secretLink(t), 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	exists, err := email.AccountExists("ann@example.com", "usr_1", signIn, recovery)
	if err != nil {
		t.Fatal(err)
	}
	passwordReset, err := email.PasswordReset("ann@example.com", "usr_1", resetLink, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	test, err := email.TestMessage("ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		message        email.Message
		kind           email.Kind
		subject        string
		contains       []string
		doesNotContain []string
	}{
		{confirm, email.KindRegistrationConfirmation, "Подтвердите регистрацию в магазине «Слой»", []string{"https://shop.example.com/register/confirm#t=" + secretToken, "48 часов", "пароль, который вы указали"}, []string{"Анна"}},
		{exists, email.KindAccountExists, "Аккаунт в магазине «Слой» уже существует", []string{"https://shop.example.com/login", "https://shop.example.com/forgot-password", "не изменён"}, []string{"#t=", "reset-password"}},
		{passwordReset, email.KindPasswordReset, "Восстановление пароля в магазине «Слой»", []string{"https://shop.example.com/reset-password#t=" + secretToken, "1 час", "все открытые сеансы будут завершены"}, nil},
		{test, email.KindTest, "Тестовое письмо магазина «Слой»", []string{"тестовое письмо"}, []string{"http"}},
	} {
		if tc.message.Kind != tc.kind || tc.message.Subject != tc.subject {
			t.Errorf("%s: kind=%s subject=%q", tc.kind, tc.message.Kind, tc.message.Subject)
		}
		for _, want := range tc.contains {
			if !strings.Contains(tc.message.Body, want) {
				t.Errorf("%s body lacks %q", tc.kind, want)
			}
		}
		for _, unwanted := range tc.doesNotContain {
			if strings.Contains(tc.message.Body, unwanted) {
				t.Errorf("%s body contains %q", tc.kind, unwanted)
			}
		}
	}
	// A template never renders without a link from the origin.
	if _, err := email.PasswordReset("ann@example.com", "usr_1", email.Link{}, time.Hour); !errors.Is(err, email.ErrInvalidLink) {
		t.Fatal("rendered without a link")
	}
	if _, err := email.TestMessage("Not An Address"); !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatal("invalid recipient accepted")
	}
	for d, want := range map[time.Duration]string{time.Hour: "1 час", 2 * time.Hour: "2 часа", 5 * time.Hour: "5 часов", 21 * time.Hour: "21 час", 48 * time.Hour: "48 часов", 30 * time.Minute: "30 минут", 90 * time.Minute: "90 минут"} {
		m, _ := email.PasswordReset("ann@example.com", "usr_1", resetLink, d)
		if !strings.Contains(m.Body, " "+want+" ") {
			t.Errorf("%v: body lacks %q", d, want)
		}
	}
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadConfig(t *testing.T) {
	productionSMTP := map[string]string{
		"EMAIL_TRANSPORT": "smtp", "SMTP_HOST": "smtp.provider.example", "SMTP_TLS": "implicit",
		"SMTP_USERNAME": "user", "SMTP_PASSWORD": "SENTINEL-SMTP-PASSWORD", "SMTP_FROM": "shop@example.com",
		"PUBLIC_SHOP_ORIGIN": "https://shop.example.com",
	}
	with := func(base map[string]string, changes map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range changes {
			if v == "<unset>" {
				delete(out, k)
			} else {
				out[k] = v
			}
		}
		return out
	}
	config, err := email.LoadConfig(env(productionSMTP), true)
	if err != nil || config.SMTP.Port != 465 || config.SMTP.TLS != email.TLSImplicit || config.PublicOrigin.String() != "https://shop.example.com" || !config.Enabled() {
		t.Fatalf("production implicit TLS: %+v %v", config.SMTP.Port, err)
	}
	if strings.Contains(config.Describe(), "SENTINEL") || strings.Contains(config.Describe(), "user") || strings.Contains(config.Describe(), "smtp.provider") {
		t.Fatal("description exposes configuration values")
	}
	if config, err := email.LoadConfig(env(with(productionSMTP, map[string]string{"SMTP_TLS": "starttls"})), true); err != nil || config.SMTP.Port != 587 {
		t.Fatalf("production STARTTLS: %v", err)
	}
	if config, err := email.LoadConfig(env(with(productionSMTP, map[string]string{"SMTP_TLS": "starttls", "SMTP_PORT": "2525"})), true); err != nil || config.SMTP.Port != 2525 {
		t.Fatalf("explicit port: %v", err)
	}
	if config, err := email.LoadConfig(env(map[string]string{}), true); err != nil || config.Enabled() {
		t.Fatalf("email disabled: %v", err)
	}
	dev := map[string]string{"EMAIL_TRANSPORT": "smtp", "SMTP_HOST": "mailpit", "SMTP_PORT": "1025", "SMTP_TLS": "none", "SMTP_FROM": "shop@example.com", "PUBLIC_SHOP_ORIGIN": "http://127.0.0.1:8080"}
	if _, err := email.LoadConfig(env(dev), false); err != nil {
		t.Fatalf("development Mailpit: %v", err)
	}
	if config, err := email.LoadConfig(env(map[string]string{"EMAIL_TRANSPORT": "log", "PUBLIC_SHOP_ORIGIN": "http://localhost:3000"}), false); err != nil || config.Transport != email.TransportLog {
		t.Fatalf("development log: %v", err)
	}
	for name, tc := range map[string]struct {
		values     map[string]string
		production bool
	}{
		"production log":                  {map[string]string{"EMAIL_TRANSPORT": "log", "PUBLIC_SHOP_ORIGIN": "https://shop.example.com"}, true},
		"production plaintext":            {with(productionSMTP, map[string]string{"SMTP_TLS": "none"}), true},
		"production anonymous":            {with(productionSMTP, map[string]string{"SMTP_USERNAME": "<unset>", "SMTP_PASSWORD": "<unset>"}), true},
		"production http origin":          {with(productionSMTP, map[string]string{"PUBLIC_SHOP_ORIGIN": "http://shop.example.com"}), true},
		"origin with path":                {with(productionSMTP, map[string]string{"PUBLIC_SHOP_ORIGIN": "https://shop.example.com/shop"}), true},
		"origin with userinfo":            {with(productionSMTP, map[string]string{"PUBLIC_SHOP_ORIGIN": "https://a:b@shop.example.com"}), true},
		"missing origin":                  {with(productionSMTP, map[string]string{"PUBLIC_SHOP_ORIGIN": "<unset>"}), true},
		"unknown TLS mode":                {with(productionSMTP, map[string]string{"SMTP_TLS": "opportunistic"}), true},
		"missing TLS mode":                {with(productionSMTP, map[string]string{"SMTP_TLS": "<unset>"}), true},
		"bad port":                        {with(productionSMTP, map[string]string{"SMTP_PORT": "70000"}), true},
		"host with port":                  {with(productionSMTP, map[string]string{"SMTP_HOST": "smtp.provider.example:465"}), true},
		"password without username":       {with(productionSMTP, map[string]string{"SMTP_USERNAME": "<unset>"}), true},
		"from not an address":             {with(productionSMTP, map[string]string{"SMTP_FROM": "Shop <shop@example.com>"}), true},
		"from with CRLF":                  {with(productionSMTP, map[string]string{"SMTP_FROM": "shop@example.com\r\nBcc: x@evil.example"}), true},
		"unknown transport":               {with(productionSMTP, map[string]string{"EMAIL_TRANSPORT": "sendgrid"}), true},
		"SMTP settings without transport": {with(productionSMTP, map[string]string{"EMAIL_TRANSPORT": "<unset>"}), true},
		"credentials over plaintext":      {with(dev, map[string]string{"SMTP_USERNAME": "user", "SMTP_PASSWORD": "SENTINEL-SMTP-PASSWORD"}), false},
		"dev origin malformed":            {with(dev, map[string]string{"PUBLIC_SHOP_ORIGIN": "http://127.0.0.1:8080/x"}), false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := email.LoadConfig(env(tc.values), tc.production)
			if err == nil {
				t.Fatal("accepted")
			}
			for _, secret := range []string{"SENTINEL-SMTP-PASSWORD", "smtp.provider.example", "shop@example.com", "evil"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error exposes %q: %v", secret, err)
				}
			}
		})
	}
}

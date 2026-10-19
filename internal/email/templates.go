package email

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/*.txt
var templateFiles embed.FS

var templates = template.Must(template.New("email").Option("missingkey=error").ParseFS(templateFiles, "templates/*.txt"))

// subjects are fixed per kind; no caller-supplied text reaches a header.
var subjects = map[Kind]string{
	KindRegistrationConfirmation: "Подтвердите регистрацию в магазине «Слой»",
	KindAccountExists:            "Аккаунт в магазине «Слой» уже существует",
	KindPasswordReset:            "Восстановление пароля в магазине «Слой»",
	KindTest:                     "Тестовое письмо магазина «Слой»",
}

// RegistrationConfirmation is the confirmation email for a pending
// registration; link carries the token in its fragment. It quotes nothing the
// registrant typed: anyone can register any address, so a name would let a
// stranger put their own text into someone else's mailbox.
func RegistrationConfirmation(to, ref string, link Link, expiresIn time.Duration) (Message, error) {
	return render(KindRegistrationConfirmation, to, ref, struct {
		Link, ExpiresIn string
	}{link.value, russianDuration(expiresIn)}, link)
}

// AccountExists answers a registration for an address that already has an
// account. It carries no token: it links to sign-in and to the recovery
// request page, where a reset link can be asked for.
func AccountExists(to, ref string, signIn, recovery Link) (Message, error) {
	return render(KindAccountExists, to, ref, struct{ SignInLink, RecoverLink string }{signIn.value, recovery.value}, signIn, recovery)
}

// PasswordReset carries a reset link with the token in its fragment.
func PasswordReset(to, ref string, link Link, expiresIn time.Duration) (Message, error) {
	return render(KindPasswordReset, to, ref, struct{ Link, ExpiresIn string }{link.value, russianDuration(expiresIn)}, link)
}

// TestMessage is the harmless message of `shopctl email test`.
func TestMessage(to string) (Message, error) {
	return render(KindTest, to, "", struct{}{})
}

func render(kind Kind, to, ref string, data any, links ...Link) (Message, error) {
	for _, link := range links {
		if link.value == "" {
			return Message{}, fmt.Errorf("%w: %s needs a link from the public origin", ErrInvalidLink, kind)
		}
	}
	var body strings.Builder
	if err := templates.ExecuteTemplate(&body, string(kind)+".txt", data); err != nil {
		return Message{}, fmt.Errorf("render %s email: %w", kind, err)
	}
	message := Message{Kind: kind, Ref: ref, To: to, Subject: subjects[kind], Body: body.String()}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

// russianDuration renders whole hours or minutes, e.g. "48 часов", "1 час",
// "30 минут".
func russianDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		hours := int(d / time.Hour)
		return fmt.Sprintf("%d %s", hours, plural(hours, "час", "часа", "часов"))
	}
	minutes := max(1, int(d/time.Minute))
	return fmt.Sprintf("%d %s", minutes, plural(minutes, "минуту", "минуты", "минут"))
}

func plural(n int, one, few, many string) string {
	switch mod100 := n % 100; {
	case mod100 >= 11 && mod100 <= 14:
		return many
	case n%10 == 1:
		return one
	case n%10 >= 2 && n%10 <= 4:
		return few
	default:
		return many
	}
}

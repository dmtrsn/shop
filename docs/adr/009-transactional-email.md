# ADR 009: Transactional email foundation

Status: Accepted for Phase A (slice A6). Date: 2026-10-16.

## Context

Self-registration (A7) and self-service password recovery (A8) need email: a registration confirmation, an "account already exists" notice and a password-reset link. Two of those carry secret, single-use tokens. The repository had no email infrastructure and no provider.

## Decision

`internal/email` provides:

- `Mailer` with one method, `Send(ctx, Message)`. A `Message` is a recipient, a fixed subject and a plain-text UTF-8 body, plus a template kind and an internal reference used for logs. `Validate` rejects anything that could alter SMTP headers; `String`, `GoString` and `LogValue` redact the recipient, subject and body.
- Fixed templates for the three customer messages and `shopctl email test`. Since A7 the confirmation quotes nothing the registrant typed, and the "account exists" notice links only to sign-in until recovery exists (ADR 010). Links are a `Link` type that only `PublicOrigin.Link` creates, so they always use `PUBLIC_SHOP_ORIGIN`, never `Host` or `X-Forwarded-Host`. The origin must be one of `TRUSTED_ORIGINS`; tokens go in the URL fragment.
- `SMTPMailer` on `net/smtp` and `crypto/tls`, provider-neutral:
  - `SMTP_TLS=implicit` (465) or `starttls` (587) with certificate and hostname verification, TLS 1.2+. STARTTLS is mandatory in that mode; there is no plaintext fallback.
  - Credentials are sent only over a verified TLS connection; `none` (plaintext) is development-only and never carries credentials.
  - Errors keep only the stage and SMTP reply code, because server text can echo recipients or credentials. 4xx replies, timeouts and dropped connections are temporary; 5xx, TLS failures and anything unknown are permanent.
- `Dispatcher`: a bounded in-process queue (100 messages, 2 workers), 3 attempts for temporary failures with 2 s and 10 s backoff, 20 s per attempt, and a process-wide cap of 60 messages per minute (burst 10). `Enqueue` never blocks: a full queue returns `ErrQueueFull` and logs the drop at error level. `Shutdown` stops intake, drains until a deadline (10 s in the server), then cancels and reports what was dropped.
- `LogMailer` for development: it logs the kind and reference only, never the recipient or body. Mailpit, under the Compose `mail` profile, is the supported way to read real messages locally and in CI.

The server validates the configuration at startup and owns the dispatcher's lifecycle; with `EMAIL_TRANSPORT` empty, email is disabled and the shop works as before. Production refuses the log transport, plaintext SMTP, anonymous SMTP and a non-https origin.

## Delivery semantics

Delivery is **not durable and not exactly-once**. A queued or in-flight message exists only in process memory and is lost on restart or after the drain deadline; each replica has its own queue and cap. That is acceptable for these messages because every state they serve is recoverable by asking again: the customer can resend the confirmation or request another reset. Keeping secret tokens only in memory also means no raw token is ever written to the database or logs. A durable outbox belongs to Phase B order notifications, which carry no secrets.

## Operations

SMTP accepting a message does not mean it reaches inboxes. The sender domain needs SPF, DKIM and DMARC, and the provider account needs to be set up; the repository configures none of that. `shopctl email test --to ADDRESS` verifies the SMTP settings before launch.

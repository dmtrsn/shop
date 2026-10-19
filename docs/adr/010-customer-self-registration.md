# ADR 010: Customer self-registration with mandatory email confirmation

Status: Accepted for Phase A (slice A7). Date: 2026-10-17.

## Context

The shop is an open public store, so customers register themselves (decision D1). An account must not exist before its email address is confirmed, the answer must not reveal whether an address already has an account, and anonymous callers must not be able to flood a mailbox or the CPU. Operator-created accounts (`shopctl customer`) remain the support and break-glass path. Password recovery is a later slice (A8).

## Decision

### State

Migration `010_customer_self_registration` adds two tables and touches nothing existing:

- `customer_registrations`: one row per normalized address (the primary key), holding the name, optional phone, the bcrypt hash (cost 12) of the chosen password, the SHA-256 digest of the confirmation token, `created_at` and `expires_at`. It has no foreign key to `users`, because the account does not exist yet. Check constraints require a lowercase address, a bcrypt-shaped hash and a 43-character digest. The token digest is unique, and `expires_at` is indexed for cleanup.
- `customer_email_throttle`: per normalized address, the send times inside the current 24-hour window and the last send time.

`users` keeps its meaning: a confirmed self-registration or an operator-created customer. There is no `users.email_verified_at`, no pending status and no generic token table; pending is not a kind of account, and disabled stays a separate concept.

### Flow

- `POST /api/auth/registration {email, password, name, phone?}` validates with the operator rules (A3), hashes the password, then in one transaction takes a per-address advisory lock (`pg_advisory_xact_lock(class, hashtext(email))`) and claims one email from the address's allowance:
  - **New or pending address:** the pending row is created or replaced, together with the password, the token and the expiry. The earlier link stops working, and a confirmation email is queued.
  - **Existing account:** nothing is stored and an "account exists" notice is queued.
  - **Throttled:** nothing changes.

  Every syntactically valid request answers `202 {"status":"pending"}` with the same headers.
- `POST /api/auth/registration/resend {email}` gives an unexpired pending registration a new token, or repeats the notice for an account, under the same allowance. It never creates a registration, since that needs the password.
- `POST /api/auth/registration/confirm {token, password}` proceeds in four steps:
  1. Read the registration by token digest. An unknown or expired token gets `410 link_invalid`.
  2. Verify the password with bcrypt, outside any transaction. A wrong password gets `401`, and the token stays usable.
  3. In one transaction, `DELETE … WHERE token_hash=? AND password_hash=? AND expires_at>now RETURNING …` claims the registration, and `INSERT INTO users … ON CONFLICT DO NOTHING` creates the customer.
  4. Start a cookie session through the same code path as login (`Service.startSession`). The session is bound to the stored hash, the cookie and response are identical to `POST /api/auth/session`, and a presented cookie is replaced.

All three endpoints require a trusted `Origin` and no CSRF token, since there is no session yet, and all answer `no-store`.

### Tokens and links

The token is 32 random bytes in base64url (43 characters). Only `SHA-256("sweetshop/registration-confirmation/v1\0" + token)` is stored, in a namespace separate from session hashes and CSRF tokens. It lasts 48 hours, and each address has one active token. The link is `PUBLIC_SHOP_ORIGIN + /register/confirm#t=<token>`, built only through `email.PublicOrigin.Link`, never from `Host`, `X-Forwarded-Host` or `Origin`. Fragments are never sent to servers or put in `Referer` headers.

The storefront reads the fragment once on the first client render, removes it with `history.replaceState`, and keeps the token only in a component ref. It never reaches storage, cookies, query parameters, server props or logs. The confirmation email quotes nothing the registrant typed; anyone can register any address, so a name would let strangers write into other people's mailboxes. The "account exists" notice links to `/login` and, since A8, to `/forgot-password` (ADR 011).

### Concurrency

Everything that can send email for an address serializes on its advisory lock, on every replica. Two registrations for one address therefore produce one pending row and one email. Two confirmations of one link race on the `DELETE`, and exactly one gets the row. An operator account created at any point wins: registration sees it and stores nothing, or confirmation's `INSERT` finds it, consumes the registration and answers `link_invalid`. No path can update or replace an existing `users` row.

### Abuse controls

- **Per address (PostgreSQL, shared by replicas):** at most one account email a minute and ten in any sliding 24 hours, claimed by one `INSERT … ON CONFLICT DO UPDATE … WHERE` statement. A throttled request still answers 202. Many source IPs cannot get around it.
- **Per client (edge):** `customer_signup` limits registration to 3 a minute with a burst of 5, and `customer_email` limits resend to 3 a minute with a burst of 3. Confirmation verifies a password, so it shares `customer_auth` with sign-in. The keys are method-aware, as in ADR 008, and the same client-IP caveat applies.
- **CPU:** a process-local bcrypt guard allows one operation per CPU, up to four waiters per CPU and a one-second wait; beyond that the request gets `503 service_unavailable` with `Retry-After: 2`. It covers every request-path bcrypt operation: registration hashing, confirmation, customer sign-in (including the dummy-hash comparison for unknown addresses, so an unknown address is never cheaper) and staff sign-in. Operator commands use it too. There is no CAPTCHA.

### Availability

The routes are registered, and `GET /api/auth/session` reports `selfService: true`, only when the server has PostgreSQL storage and a running email dispatcher. Without email, or on the memory/demo adapter, the routes do not exist (404 like any unknown `/api` path) and the storefront shows no registration link. Production cannot run with self-registration enabled and email disabled.

The email queue is in memory (ADR 009). If a message is refused after the state change, the registration stands, the refusal is logged without address, token or body, the client still gets 202, and the customer recovers with resend.

### Cleanup

The server deletes expired registrations, and allowance rows whose last email left the window, every 15 minutes. It works in batches of 500 (at most 20 per run) using `FOR UPDATE SKIP LOCKED`, so replicas running it at the same time never wait on or double-delete a row. It never touches `users`.

## Contract

`api/openapi.yaml` marks the three operations `x-self-service`. The shared contract run (memory adapter, and PostgreSQL without email) requires them to be absent and `selfService` false; `contract.RunSelfService` covers them against PostgreSQL with a recording outbox. `link_invalid` (410) and `service_unavailable` (503) are new error codes.

## Rollback

Migration 010 is additive. An A6 image accepts the A7 schema (its migrator tolerates the newer version) and simply lacks the routes; pending registrations are invisible to it and cannot sign in. Rolling forward makes unexpired links work again. No production down migration is provided.

## Not in this slice

Password recovery (ADR 011), profile editing, guest carts, admin UI, MFA and social login.

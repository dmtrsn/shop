# ADR 011: Self-service password recovery

Status: Accepted for Phase A (slice A8). Date: 2026-10-19.

## Context

Customers register themselves (ADR 010), so they must also recover a forgotten password themselves, without staff. Recovery has to end every existing sign-in, must not sign anyone in, must not reveal whether an address has an account, and must not let a disabled account back in. `shopctl customer set-password` stays as the support and break-glass path.

## Decision

### State

Migration `011_customer_password_resets` adds one table and touches nothing existing. `customer_password_resets` is keyed by `user_id`, with a foreign key to `users` and `ON DELETE CASCADE`; users are disabled, never deleted. The key makes the row the customer's single active token. It also holds the SHA-256 digest of the emailed token (unique, 43 characters checked), `created_at` and `expires_at` (indexed for cleanup). There is no generic token table, and `users` is unchanged.

### Tokens and links

The token is 32 random bytes in base64url, the same shape as a registration token. The database stores only `SHA-256("sweetshop/password-reset/v1\0" + token)`, a namespace separate from registration confirmations, session token hashes and CSRF derivations, so no token works for another purpose. It lasts 60 minutes, works once, and a newer request replaces it. The link is `PUBLIC_SHOP_ORIGIN + /reset-password#t=<token>`, built only through `email.PublicOrigin.Link`, never from `Host`, `X-Forwarded-Host`, `Forwarded` or `Origin`. The storefront takes the fragment on the first client render, removes it with `history.replaceState`, and keeps the token only in a component ref, using the same helper as `/register/confirm`. The page posting to a given endpoint decides the purpose.

### Request: `POST /api/auth/password-reset {email}`

The request requires a trusted `Origin` and no session or CSRF token, and answers `no-store`. Every syntactically valid address gets the same `202 {"status":"pending"}`, with the same headers as a registration:

- In one transaction it takes the per-address advisory lock (shared with registration) and then reads the `users` row `FOR SHARE`.
- An enabled customer whose allowance permits one more email gets its reset row created or replaced, and the reset email is queued.
- Unknown and disabled addresses change nothing and claim no allowance. A throttled address changes nothing.
- No branch hashes a password, so none is a bcrypt operation cheaper than another. The remaining difference is one or two SQL statements; equal timing is not claimed.
- The allowance is A7's `customer_email_throttle`: one account email a minute and ten in any sliding 24 hours per address, shared by registration, resend and reset across replicas. Alternating the flows does not reset the budget.
- If the in-memory queue refuses the message, the token stands, the password is untouched, the refusal is logged without address, token or body, and the client still gets 202. The customer asks again once the allowance permits.

### Confirmation: `POST /api/auth/password-reset/confirm {token, password}`

It requires a trusted `Origin` and no session or CSRF token. The order of checks avoids any account-state oracle:

1. **Token shape and lookup.** A malformed, unknown, used, replaced or expired token gets `410 link_invalid`, before anything else is checked.
2. **Password policy.** The A3 customer rules apply. A violation gets `422`, so only someone holding a usable link ever sees it.
3. **Hashing.** bcrypt at cost 12 under the A7 guard, outside any transaction. Overload gets `503 service_unavailable` with `Retry-After: 2`; it is reachable only with a usable link, so it says nothing about an account.
4. **One transaction:**
   - lock the `users` row `FOR NO KEY UPDATE`, the mode of cart writers and of the password `UPDATE` itself;
   - `DELETE` the reset row where it still holds the digest and has not expired (of two concurrent confirmations, one wins);
   - if the account is disabled, commit the consumed token and answer `410`;
   - otherwise `UPDATE users SET password_hash`, `DELETE FROM sessions WHERE user_id` (cookie and bearer, every device) and commit.

Success is `204` with no body. No session is created. The server also ends the session the browser presented, whoever it belonged to, and clears the cookie; the storefront then offers `/login`. Customer data (cart, favorites, orders) is untouched.

### Lock order and races

Every account change locks the `users` row before the customer's other rows:

- **Reset request:** advisory lock, then `users` (`FOR SHARE`), then the throttle and reset rows.
- **Reset confirmation:** `users` (`FOR NO KEY UPDATE`), then the reset row, then sessions.
- **Operator disable, enable and set-password:** `UPDATE users`, then sessions, then the reset row. These now delete an outstanding reset too, so a disabled account cannot recover after a later enable, and an emailed link cannot override an operator's password.
- **Cart writers:** `users` (`FOR NO KEY UPDATE`) first.
- **Login's `CreateSession`:** `users` (`FOR SHARE`) together with its hash guard.
- **Cleanup:** `FOR UPDATE SKIP LOCKED`, so it never waits.

No cycle can form. A login that verified the old password while a reset holds the row waits on that row lock, then sees the new hash and is refused (A3's hash-bound session guard). A login that finished earlier has its session deleted by the reset.

### Availability

Recovery shares the `selfService` capability with registration: both need PostgreSQL and a running email dispatcher, and both exist or neither does (`httpapi.WithSelfService` takes both). A separate flag would only allow a state where registration works but recovery silently doesn't, or the reverse. Without email, or on the memory/demo adapter, the routes are absent (404), and `/login` shows neither «Создать аккаунт» nor «Забыли пароль?».

### Edge limits

- `POST /api/auth/password-reset` shares `customer_email` (3 a minute, burst 3) with registration resend.
- `POST /api/auth/password-reset/confirm` uses its own zone, `customer_token`: 6 a minute, burst 6, `Retry-After: 10`. It is gated by an emailed token and hashes a password, but is not a guessable credential check. Registration confirmation stays on `customer_auth`.
- Keys are method-aware, nginx answers 429 before Go, and the ADR 008 client-IP caveat applies unchanged.

### Cleanup

The self-service cleanup loop (every 15 minutes, batches of 500, `SKIP LOCKED`) also deletes expired resets. It never touches `users`.

## Rollback

Migration 011 is additive. An A7 image accepts the A8 schema (its migrator tolerates the newer version), lacks the reset routes, and signs customers in normally. Outstanding links stay unusable until the roll-forward, and work again afterwards if they have not expired. No production down migration is provided.

## Not in this slice

Changing the password from the account page, email change, profile editing, per-link attempt counters, and recovery for staff (operators use `shopctl staff set-password`).

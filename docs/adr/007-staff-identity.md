# ADR 007: Separate staff identity for 1.0

Status: Accepted for Phase A (slice A4). Date: 2026-10-16. Refines ADR 006; does not replace it.

## Decision

Staff are a principal separate from customers at every layer:

| Concern | Customer | Staff |
| --- | --- | --- |
| Accounts | `users` | `staff_users` (`stf_` + 128 random bits, unique `lower(email)`, disabled not deleted) |
| Sessions | `sessions` | `staff_sessions` (SHA-256 digest only, FK `ON DELETE RESTRICT`, 12 h absolute, no idle timeout) |
| Cookie | `__Host-sweetshop_session`, SameSite=Lax | `__Host-sweetshop_staff`, SameSite=Strict (`sweetshop_staff` without Secure in development) |
| CSRF | SHA-256 of `sweetshop/csrf/v1\0` + token | SHA-256 of `sweetshop/staff-csrf/v1\0` + token |
| Origins | `TRUSTED_ORIGINS` | `STAFF_TRUSTED_ORIGINS`; overlap is a startup error |
| Transport | cookie or legacy bearer | cookie only; any `Authorization` header is 401 |
| Expected principal | `X-Expected-Authenticated-User-ID` | `X-Expected-Staff-ID` |
| Context | `currentUser` (`domain.User`) | `currentStaff` (`domain.StaffUser`, another key) |
| API | `api/openapi.yaml` | `api/admin-openapi.yaml`, `/api/admin/v1/*` |

There is no shared principal abstraction; only small cryptographic helpers are shared.

Staff accounts are created and maintained only with `shopctl staff`, with passwords read from standard input (at least 12 characters, bcrypt cost 12). Login answers unknown, wrong-password and disabled accounts identically, binds the new session to the verified hash under a `FOR SHARE` lock on the staff row, and replaces a presented staff session. `set-password`, `disable` and `enable` revoke every staff session of the account in the same transaction.

The staff API exists only with PostgreSQL storage and a non-empty `STAFF_TRUSTED_ORIGINS`; otherwise its routes are not registered (404). Every staff operation in Phase B must go through `StaffAuth.Authorize(staff, action)`.

## Admin origin

The target remains a separate admin origin (ADR 006). Until that host exists, the shop edge returns 404 for `/api/admin` and `/api/admin/*` itself, so the staff API is unreachable through the shop host whatever Go is configured with. The admin server block and its TLS belong to later slices; its sign-in limit is already defined (`staff_auth`, [ADR 008](008-edge-sign-in-rate-limits.md)) and must be attached before the admin origin is exposed.

## Deliberate 1.0 deviations from ADR 006

- **Single role.** Every active staff account holds every staff permission. There are no role or permission tables; `Authorize` is the single place where they would be enforced.
- **No MFA.** Deferred. It must be revisited before the admin origin is exposed to the internet; until then, the admin origin should be reachable only through a restricted network path.
- **No audit log yet.** There are no staff mutations in A4. Phase B adds transactional audit rows before the first one. `shopctl` emits structured operator logs meanwhile.
- **No idle timeout and no reauthentication** for sensitive actions in 1.0.

## Rollback

Migration 009 only adds tables. Earlier images ignore them and never register staff routes; the A1+ migrator in those images treats 009 as a newer version. Rolling back therefore removes staff access and nothing else.

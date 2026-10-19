# ADR 012: Transactional staff audit

Status: Accepted for Phase B (slice B0). Date: 2026-10-19. Implements the audit part of ADR 006 and closes the "no audit log yet" deviation of ADR 007 before the first staff business mutation exists.

## Context

Phase B adds staff writes to catalog and orders. ADR 006 requires every staff write and its audit record to commit together. If the audit mechanism arrived after the first write, that write would either go unaudited or be audited by a second, separately committed step, and a crash between the two would leave a change nobody can attribute. B0 therefore ships the mechanism alone: no business mutation, no HTTP route, no UI.

## Decision

### Invariant

A staff business mutation and its audit row commit in one PostgreSQL transaction, or neither does. No audit row means no committed mutation; a failed mutation leaves no audit row. Audit failure is a hard failure of the mutation, never a logged warning.

### Table: `staff_audit_events` (migration `012_staff_audit`)

| Column | Why |
| --- | --- |
| `id` | `aud_` + 128 random bits in hex, generated with `crypto/rand`, like `stf_` IDs. Opaque: it reveals no count or order. |
| `staff_user_id` | The actor. `NOT NULL`, FK to `staff_users` with `ON DELETE RESTRICT`; staff are disabled, never deleted, so history keeps its actor. |
| `action` | `"<entity>.<verb>"`, two lowercase snake_case words of up to 32 characters (`product.update`, `order.status_change`). Checked in Go and by a `CHECK`. |
| `entity_type`, `entity_id` | The resource changed: snake_case type, ID of 1–128 characters of `[A-Za-z0-9._:-]` (fits `p01`, `ord-…`, `prd_…`). A reference, not an FK: history stays readable after a resource is archived, and the audit table knows no business schema. |
| `request_id` | Correlation with the request log. Nullable for processes without a request. Same character set and bound as `entity_id`. |
| `created_at` | `DEFAULT now()`: the database clock at transaction start, never a client timestamp. |

Indexes: `(staff_user_id, created_at)` for "what did this person do" and `(entity_type, entity_id, created_at)` for "history of this resource". No `created_at`-only index until a global feed exists. Ordering of events with equal `created_at` (one transaction) is unspecified.

Not stored: client IP (its provenance is still deployment-dependent, ADR 008), user agent, or any free-form details.

### Details and before/after data: deferred

B0 has no `details` column. Every useful detail is entity-specific (which product fields changed, an order's previous and next status), and a generic JSON field without a consumer would invite dumping request bodies or domain objects. The first slice that needs details adds a nullable column in its own migration, filled only from a typed, per-action struct of named scalar fields with a size bound, and never with passwords, hashes, session/CSRF/reset/registration tokens, SMTP credentials, cookies or `Authorization` values. Until then the audit types hold identifiers only, and a unit test pins their exact fields.

### Actor and authorization

`domain.StaffMutation` carries actor, action and request ID. Its fields are unexported, so it is built only by `domain.NewStaffMutation(staff domain.StaffUser, …)`: the actor comes from a staff principal value, never a string, and must match `stf_` + 32 hex digits, so a customer ID cannot become an actor (the FK refuses one as well). `StaffAuth.AuthorizeMutation` calls `StaffAuth.Authorize`, which stays the single authorization entrypoint, and then builds the mutation. In HTTP, `Server.staffMutation(r, action)` takes the principal that `staffAuth` stored from the staff cookie and the request ID from the request context; no request body field reaches it.

### Transaction API

`postgres.Store.InStaffMutation(ctx, mutation, work)`:

1. locks the actor's `staff_users` row `FOR SHARE`; a missing or disabled actor is `domain.ErrAuditActorInactive` (an `ErrUnauthorized`) before any business statement, and a concurrent disable or password change waits for the commit;
2. calls `work(ctx, tx)`, which performs the business change and returns its `domain.AuditTarget`;
3. inserts the audit row in the same transaction;
4. commits.

Any error from `work`, the target check, the insert or the commit (and any panic) rolls back everything. `work` receives a `postgres.StaffTx`, which exposes only `Exec`, `Query` and `QueryRow`: it cannot commit, roll back or open a nested transaction, so it cannot commit ahead of its audit row. Its SQL must not contain transaction-control statements either. The insert itself (`appendStaffAudit`) is unexported, takes the caller's `pgx.Tx`, and never begins or commits a transaction; there is no other way to write the table.

This follows the existing store pattern (each store method owns one transaction; `pgx.Tx` never leaves `internal/repository/postgres`), adding one helper rather than a unit-of-work framework.

### Request ID

The existing request ID is reused: the `X-Request-ID` the client sent, or 16 random hex digits. Because the client may choose it, it is a correlation aid, not evidence, and it is not unique. `NewStaffMutation` rejects a present request ID outside the stored shape with a `422` on `X-Request-ID`, so a staff mutation never stores a value different from the one logged. Customer routes keep accepting any request ID as before.

### Append-only

There is no store method or route that updates or deletes audit rows. In the database, `BEFORE UPDATE OR DELETE` (row) and `BEFORE TRUNCATE` (statement) triggers raise `restrict_violation`. The application role owns the table and could drop the triggers, so this guards against accidental SQL, not against a compromised application or database role. Separating a read/insert-only runtime role from the migration owner is a later deployment hardening step. No hash chain or other tamper evidence is built.

### Retention

Rows are kept indefinitely; there is no cleanup job or TTL. Production retention is a later business and compliance decision. The down migration drops the table and exists for development only.

### Not business audit

- **Authentication events** (staff sign-in, sign-out, session lookups, failures) stay in the structured request and security logs. Recording staff sign-in as security audit may come later as its own decision.
- **`shopctl` operator commands** (`customer …`, `staff …`, `migrate`) are operator tooling that predates staff business mutations. They have no staff principal and keep their structured operator logs. Auditing them would be a separate, deliberate decision.
- **No audit HTTP API or UI** in B0. Browsing belongs with the admin UI roadmap, behind its own authorization.

## Pattern for B1 and later

An application port takes the `domain.StaffMutation`, so a store method cannot be called without an authorized actor, and the PostgreSQL method wraps its statements in `InStaffMutation`:

```go
// HTTP handler, behind staffAuth (and, once the admin host exists, its edge limits).
mutation, err := s.staffMutation(r, "product.update") // principal + Authorize + request ID
if err != nil { s.writeError(w, r, err); return }
product, err := s.products.Update(r.Context(), mutation, input) // validation, then the store

// internal/repository/postgres
func (s *Store) UpdateProduct(ctx context.Context, mutation domain.StaffMutation, change domain.ProductChange) (domain.Product, error) {
	var product domain.Product
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		// lock and read, apply the change, using tx only
		if err := tx.QueryRow(ctx, `UPDATE products SET … WHERE id=$1 AND revision=$2 RETURNING …`, …).Scan(…); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: "product", ID: product.ID}, nil
	}) // the audit row is inserted here, in the same tx, then everything commits
	return product, err
}
```

The audit row is written by `InStaffMutation` in the same transaction; the mutation code neither appends nor commits. Never call the pool from inside `work`, never run a staff business mutation outside `InStaffMutation`, and define each action name as a constant of the slice that implements it.

## Consequences

- Audit adds one row lock and one insert per staff mutation.
- A disable or password change waits for a running staff mutation of that account, and a disabled actor cannot commit one. A session revoked by a password change, while its account stays enabled, may still complete a request that authenticated before the change.
- One mutation records one target. Bulk actions will extend `work` to return several targets when they arrive.

## Rollback

Migration 012 only adds a table, two indexes, a function and two triggers. Earlier images never read them; the A1+ migrator treats 012 as a newer migration, and no earlier image writes staff business data, so no down migration is needed to roll back.

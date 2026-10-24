# ADR 014: Admin frontend and admin edge

Status: Accepted for Phase B (slice B2). Date: 2026-10-19. Implements the separate admin application and origin of ADR 006 on top of staff identity (ADR 007), the edge rate limits (ADR 008) and the staff product API (ADR 013).

## Decision

### A separate Next application

`apps/admin` is its own Next 16 app, with its own `package.json`, lock file, Dockerfile, image (`sweetshop-admin`), tests and CI job. It shares no code, bundle or route with `apps/storefront`; the dependency versions and design tokens are deliberately the same, copied rather than linked. A storefront release never ships staff code, and the admin can be rolled back or removed without touching the shop.

B2 ships only:

- the staff sign-in page (`/login`);
- an authenticated shell: identity, navigation, sign-out;
- a truthful overview page (`/`) with no metrics;
- error, not-found and loading states.

Navigation names the sections later slices add (products, orders) without linking them. B3 adds product management, starting from the B1 API ([ADR 015](015-admin-product-ui.md)).

### A separate origin and edge

| Host | Edge | Routes |
| --- | --- | --- |
| Shop (e.g. `shop.example`) | `edge`, `deploy/nginx/default.conf.template` | unchanged; `/api/admin` and `/api/admin/*` return 404 from nginx itself |
| Admin (e.g. `admin.example`) | `admin-edge`, `deploy/nginx/admin.conf.template` | `/api/admin/*` to Go; everything else to the admin app; any other `/api` path is 404 at nginx |

The admin host is never a second storefront: it serves no customer API, legacy assets or storefront pages. Go still enforces the A4 rules on every staff request: staff cookie only, `STAFF_TRUSTED_ORIGINS` for unsafe methods and sign-in, staff CSRF, `X-Expected-Staff-ID`, and 401 for any `Authorization` header. Overlapping customer and staff origins are still a startup error.

### Staff session model in the browser

The browser holds no staff secret it can read:

- **Session.** It is the HttpOnly `__Host-sweetshop_staff` cookie: Secure, SameSite=Strict, `Path=/`, no Domain. It is host-only on the admin host, so it is never shared with the shop host or other subdomains.
- **Route gate.** Pages under `src/app/(staff)` are dynamic. Their layout asks Go (`GET /api/admin/v1/auth/session`, through the private `ADMIN_API_BASE_URL`) using only the staff cookie:
  - Go says signed out: redirect to `/login`;
  - Go confirms a session: render;
  - Go is unreachable or answers badly: an error page, never "signed out".
  - `/login` redirects to `/` only when Go confirms a session.
  - The gate keeps only the identity (ID, email, display name): never the CSRF token, never the cookie.
- **CSRF.** The client fetches the token from the same endpoint and keeps it only in React memory, never in storage, a JS cookie, the URL or server HTML. A 403 on sign-out refetches it once.
- **Sign-out.** `DELETE` with `X-CSRF-Token` and `X-Expected-Staff-ID`. A 409 (another staff member is now signed in in this browser) is reported, not overridden. Customer headers such as `X-Expected-Authenticated-User-ID` are never sent.
- **Expiry.** Sessions last 12 hours with no silent re-authentication. When Go no longer recognises the session, the page drops its state and moves to `/login` once.

### Headers and caching

The admin `proxy.ts` sets a per-request nonce CSP, stricter than the storefront's, with no `unsafe-inline`, no `unsafe-eval` and no third-party sources. It also sets `Cache-Control: no-store`, `X-Robots-Tag: noindex`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` and the storefront's other headers. Hashed `/_next/static` assets keep Next's caching. Go's staff responses are already `no-store`.

### Staff sign-in rate limit

The admin edge attaches `staff_auth` (ADR 008: 5 per minute, burst 3, `nodelay`, `Retry-After: 12`) to `POST /api/admin/v1/auth/session`. Other methods and other staff routes are not limited.

The key is the TCP peer. `real_ip` is off, so `X-Forwarded-For`, `X-Real-IP` and `Forwarded` cannot open new buckets. ADR 008's caveat applies unchanged: behind another proxy, including Docker's userland proxy for a loopback-published port, every client shares that proxy's bucket. Real client-IP handling must be configured and verified before production.

The shop templates still define the zone, unattached; their `/api/admin` 404 comes first.

### Enabling it, and why it is not public yet

Compose starts `admin` and `admin-edge` only with the `admin` profile. `admin-edge` listens on `ADMIN_EDGE_BIND:ADMIN_EDGE_PORT`, default `127.0.0.1:8081`. Go must also have `STAFF_TRUSTED_ORIGINS` set to the admin edge's exact origin; empty keeps the staff API off.

```bash
COMPOSE_PROFILES=admin STAFF_TRUSTED_ORIGINS=http://localhost:8081 docker compose up -d --build
docker compose run --rm --no-deps -T --entrypoint /app/shopctl app staff create --email ops@example.com --name 'Оператор' --password-stdin
# open http://localhost:8081
```

Locally the admin uses `localhost` and the shop uses `127.0.0.1`, so their cookies never share a host. In production the admin origin is `https://` on its own host name, and `TRUSTED_ORIGINS`/`STAFF_TRUSTED_ORIGINS` stay disjoint.

**The admin is not ready for internet exposure.** ADR 007 deferred MFA and requires revisiting it before the admin origin is reachable from the internet. That launch blocker stands. Until it is resolved:

- keep `ADMIN_EDGE_BIND` on loopback or a restricted network (VPN or allowlist);
- terminate TLS in front of it;
- configure verified client-IP handling before relying on the limiter.

Nothing in the repository publishes the admin host by default.

### Development

`npm run dev` in `apps/admin` serves `:3001` and forwards only `/api/admin/*` to `ADMIN_API_BASE_URL` (default `http://127.0.0.1:8080`, a local Go server). Go then needs `STAFF_TRUSTED_ORIGINS=http://localhost:3001`. There is no staff login without PostgreSQL, no demo staff account and no hard-coded credential. Staff accounts are provisioned only with `shopctl staff`.

## Verification

- **Unit tests:** `apps/admin` covers the auth client, the server gate, the forms and the shell.
- **Isolated limiter test:** `scripts/test-staff-auth-limit.sh` loads the real admin template with stub upstreams.
- **On the real stack:**
  - `scripts/test-admin-edge.sh`: routing, headers, cookie attributes, origins, principal isolation, and shop-host 404 for every staff route;
  - `scripts/test-admin-browser-flow.mjs`: the sign-in journey in headless Chrome. Browsers reject the `__Host-` prefix over plain `http://`, even on localhost (Chrome reports `InvalidPrefix`), so it runs through `scripts/with-admin-tls-proxy.sh`. That wrapper puts a throwaway self-signed TLS terminator in front of `admin-edge`, the production topology, and Go must also trust `https://localhost:8443`;
  - `scripts/test-admin-auth-rate-limit.sh`: the burst through the real admin edge.
- **CI:** the `admin` job runs typecheck, lint, tests and build; `edge-routing` runs all of the above.

## Rollback

B2 has no migration and changes no Go code. Rolling back means stopping or removing `admin` and `admin-edge`. The shop edge, the storefront and Go are unaffected, and the B1 staff API stays unreachable from the shop host.

## Not in B2

Product, order and dictionary screens; media; roles; MFA; a public admin deployment.

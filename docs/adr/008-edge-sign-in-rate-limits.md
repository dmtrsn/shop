# ADR 008: Sign-in rate limits at the edge

Status: Accepted for Phase A (slice A5). Date: 2026-10-16.

## Decision

The shop edge (`deploy/nginx/*.conf.template`, both route maps) limits password verification before it reaches Go and bcrypt:

| Zone | Attached to | Rate | Burst | Excess | 429 `Retry-After` |
| --- | --- | --- | --- | --- | --- |
| `customer_auth` | `POST /api/auth/session`, `POST /api/auth/login` (shared bucket) | 10/min | 10, `nodelay` | rejected at once | 6 s (one slot per 6 s) |
| `staff_auth` | nothing on the shop host; the future admin host's `POST /api/admin/v1/auth/session` | 5/min | 3, `nodelay` | rejected at once | 12 s |
| `customer_signup` (A7) | `POST /api/auth/registration` | 3/min | 5, `nodelay` | rejected at once | 20 s |
| `customer_email` (A7, A8) | `POST /api/auth/registration/resend`, `POST /api/auth/password-reset` (shared bucket) | 3/min | 3, `nodelay` | rejected at once | 20 s |
| `customer_token` (A8) | `POST /api/auth/password-reset/confirm` | 6/min | 6, `nodelay` | rejected at once | 10 s |

`POST /api/auth/registration/confirm` verifies a password, so it joins the `customer_auth` bucket (ADR 010).

The key is `$binary_remote_addr` for `POST` and empty for every other method, so session reads and sign-out are never limited. `nodelay` rejects excess immediately instead of queueing it toward bcrypt. The limiter answers with a short `text/plain` 429 from nginx that says nothing about accounts or credentials; Go never sees the request.

`customer_auth` lets a client make 11 sign-in attempts at once and then one every 6 seconds, about 600 per hour: enough for retries, password managers and a few people behind one NAT, while making per-address guessing slow and keeping bcrypt cost bounded. The staff zone is stricter because a handful of staff sign in rarely.

Go is reachable only inside the Compose network; every browser sign-in passes the edge. Since A7, Go also bounds concurrent bcrypt work in each process (ADR 010), so a limiter bypass or a shared bucket can no longer occupy every CPU.

## Client-IP provenance

Today no `real_ip_header`, `set_real_ip_from` or `proxy_protocol` is configured. `$remote_addr` is therefore the TCP peer of nginx, which a client cannot forge; `X-Forwarded-For`, `X-Real-IP` and `Forwarded` never change the bucket (tested in `scripts/test-auth-rate-limit.sh`). Go does not read those headers either.

That peer is the real client only when clients reach nginx directly. With the default `APP_BIND=127.0.0.1` and a reverse proxy on the host, or a load balancer or CDN in front, every request arrives from the proxy (for a loopback-published port, Docker's userland proxy makes it the bridge gateway, as the edge test logs show). Then all clients share one bucket: no spoofing is possible, but a single attacker can exhaust everyone's sign-in budget.

**Before internet exposure behind an upstream TLS proxy, verify real client IP provenance and configure trusted proxy addresses.** Concretely, the deployment must configure, for its actual topology:

- the exact address or network of the trusted proxy (`set_real_ip_from`, never `0.0.0.0/0`);
- the exact mechanism that proxy uses (`real_ip_header X-Forwarded-For` with `real_ip_recursive on`, `X-Real-IP`, or `proxy_protocol`), matching what it actually sends;
- a runtime check that the bucket key in nginx's "limiting requests" log line is the real client address, and that a forged header from outside does not change it.

Until that is done and verified, the proxy-fronted deployment is not rate-limit-safe. Nothing in this repository solves it yet.

## Staff

The `staff_auth` zone is defined in the shop templates (zones are http-level) but attached nowhere there; the shop host returns 404 for `/api/admin` before any limiter. `scripts/test-staff-auth-limit.sh` attaches it in an isolated nginx exactly as the admin host must:

```nginx
location = /api/admin/v1/auth/session {
    limit_req zone=staff_auth burst=3 nodelay;
    error_page 429 @staff_rate_limited;   # text/plain 429, Retry-After: 12
    proxy_pass http://app:8080;
}
```

Staff sign-in must be limited this way, behind verified client-IP provenance, before the admin origin is exposed.

## Out of scope

General API rate limiting, per-account lockout (the edge does not parse request bodies, and lockout would let anyone lock a known account), IPv6 prefix aggregation (a client with many IPv6 addresses gets many buckets), and limits on future password-setup links (A6 inherits this design).

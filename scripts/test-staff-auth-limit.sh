#!/usr/bin/env bash
set -euo pipefail

# Staff sign-in limiter, tested in an isolated nginx container.
#
# The shop edge defines the staff_auth zone but attaches it nowhere: there is
# no admin host yet and the shop host answers /api/admin with 404. This test
# loads the real shop template (the zone definitions and the 404 rule) plus a
# test-only stand-in for the future admin server block that attaches the zone
# as documented, with a stub in place of Go. Nothing here changes production
# routing. Requires Docker.
template="${EDGE_TEMPLATE:-deploy/nginx/default.conf.template}"
image="${NGINX_IMAGE:-nginx:1.27-alpine}"
# Must match the documented staff attachment: rate=5r/m, burst=3, Retry-After 12.
burst=3
retry_after=12

root="$(cd "$(dirname "$0")/.." && pwd)"
temporary_dir="$(mktemp -d)"
container="staff-auth-limit-$$"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$temporary_dir"
}
trap cleanup EXIT

fail() {
  echo "staff auth limit: $1" >&2
  exit 1
}

cat >"$temporary_dir/zz-admin-stand-in.conf" <<'NGINX'
# TEST ONLY: stands in for the future admin server block.
server {
    listen 8081;
    limit_req_status 429;

    location = /api/admin/v1/auth/session {
        limit_req zone=staff_auth burst=3 nodelay;
        error_page 429 @staff_rate_limited;
        proxy_pass http://127.0.0.1:8082;
    }

    location @staff_rate_limited {
        default_type text/plain;
        add_header Retry-After 12 always;
        add_header Cache-Control no-store always;
        return 429 "Too many sign-in attempts. Try again later.\n";
    }
}

# Stands in for Go: a staff sign-in that fails.
server {
    listen 8082;
    location / {
        add_header X-Request-ID stub always;
        return 401 "stub\n";
    }
}
NGINX

docker run -d --name "$container" \
  --add-host app:127.0.0.1 --add-host storefront:127.0.0.1 -e STOREFRONT_PORT=3000 \
  -v "$root/$template:/etc/nginx/templates/default.conf.template:ro" \
  -v "$temporary_dir/zz-admin-stand-in.conf:/etc/nginx/conf.d/zz-admin-stand-in.conf:ro" \
  -p 127.0.0.1::8080 -p 127.0.0.1::8081 "$image" >/dev/null
shop="http://$(docker port "$container" 8080/tcp | head -1)"
admin="http://$(docker port "$container" 8081/tcp | head -1)"
for _ in $(seq 50); do
  curl --silent --fail "$shop/edge/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl --silent --fail "$shop/edge/healthz" >/dev/null || fail "nginx did not start: $(docker logs "$container" 2>&1 | tail -5)"

post() {
  curl --silent --path-as-is --max-time 10 --request POST --header 'Content-Type: application/json' --data '{}' \
    --dump-header "$temporary_dir/h" --output "$temporary_dir/b" --write-out '%{http_code}' "$@"
}

# The shop host never exposes staff sign-in, limited or not.
for i in $(seq 8); do
  status="$(post "$shop/api/admin/v1/auth/session")"
  [[ "$status" == 404 ]] || fail "shop host /api/admin/v1/auth/session: HTTP $status"
done

# GET is never limited.
for i in $(seq 8); do
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' "$admin/api/admin/v1/auth/session")"
  [[ "$status" == 401 ]] || fail "GET was limited: HTTP $status"
done

allowed=0
limited=0
for i in $(seq 12); do
  status="$(post --header "X-Forwarded-For: 203.0.113.$i" --header "X-Real-IP: 192.0.2.$i" --header "Forwarded: for=198.51.100.$i" "$admin/api/admin/v1/auth/session")"
  case "$status" in
    401)
      ((allowed += 1))
      ((limited == 0)) || fail "attempt $i passed after the limiter engaged"
      ;;
    429)
      ((limited += 1))
      grep -qi '^x-request-id:' "$temporary_dir/h" && fail "429 reached the upstream"
      grep -Eqi "^retry-after:[[:space:]]*$retry_after[[:space:]]*$" "$temporary_dir/h" || fail "Retry-After $retry_after missing"
      ;;
    *) fail "attempt $i: HTTP $status" ;;
  esac
done
((allowed == burst + 1)) || fail "$allowed staff attempts passed, expected $((burst + 1))"
echo "staff auth limit passed: $allowed passed, $limited rejected; the shop host returned 404"

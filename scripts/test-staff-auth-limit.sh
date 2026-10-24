#!/usr/bin/env bash
set -euo pipefail

# Staff sign-in limiter of the real admin edge template, in an isolated nginx
# container with stubs in place of Go and the admin Next app (ADR 008, ADR
# 014). Checks the attachment itself: POST to the staff session is limited
# (5r/m, burst 3, Retry-After 12) and forged forwarding headers never open new
# buckets; GET is never limited; customer API paths are 404 at this edge.
# scripts/test-admin-auth-rate-limit.sh repeats the burst against the real
# stack. Requires Docker.
template="deploy/nginx/admin.conf.template"
image="${NGINX_IMAGE:-nginx:1.27-alpine}"
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

# TEST ONLY: stubs for the upstreams. "app" resolves to 127.0.0.2, so its stub
# listens there on the edge's own port; nginx dispatches by local address.
cat >"$temporary_dir/zz-stubs.conf" <<'NGINX'
server {
    listen 127.0.0.2:8080;
    location / {
        add_header X-Request-ID stub-go always;
        return 401 "stub go\n";
    }
}
server {
    listen 3001;
    location / {
        add_header X-Admin-Runtime stub-next always;
        return 200 "stub admin\n";
    }
}
NGINX

docker run -d --name "$container" \
  --add-host app:127.0.0.2 --add-host admin:127.0.0.1 -e ADMIN_PORT=3001 \
  -v "$root/$template:/etc/nginx/templates/default.conf.template:ro" \
  -v "$temporary_dir/zz-stubs.conf:/etc/nginx/conf.d/zz-stubs.conf:ro" \
  -p 127.0.0.1::8080 "$image" >/dev/null
admin="http://$(docker port "$container" 8080/tcp | head -1)"
for _ in $(seq 50); do
  curl --silent --fail "$admin/edge/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl --silent --fail "$admin/edge/healthz" >/dev/null || fail "nginx did not start: $(docker logs "$container" 2>&1 | tail -5)"

call() {
  curl --silent --path-as-is --max-time 10 --dump-header "$temporary_dir/h" --output "$temporary_dir/b" --write-out '%{http_code}' "$@"
}

# Routing: the staff API reaches Go, the frontend owns the rest, and the
# customer API does not exist here.
[[ "$(call "$admin/api/admin/v1/products")" == 401 ]] && grep -qi '^x-request-id: stub-go' "$temporary_dir/h" || fail "staff API did not reach Go"
[[ "$(call "$admin/login")" == 200 ]] && grep -qi '^x-admin-runtime: stub-next' "$temporary_dir/h" || fail "/login did not reach the admin app"
for path in /api /api/auth/session /api/catalog /%61pi/catalog //api//catalog /api/admin; do
  status="$(call "$admin$path")"
  [[ "$status" == 404 ]] || fail "$path on the admin host: HTTP $status"
  grep -qi '^x-request-id:' "$temporary_dir/h" && fail "$path reached Go"
done

# GET is never limited.
for i in $(seq 8); do
  status="$(call "$admin/api/admin/v1/auth/session")"
  [[ "$status" == 401 ]] || fail "GET was limited: HTTP $status"
done

allowed=0
limited=0
for i in $(seq 12); do
  status="$(call --request POST --header 'Content-Type: application/json' --data '{}' \
    --header "X-Forwarded-For: 203.0.113.$i" --header "X-Real-IP: 192.0.2.$i" --header "Forwarded: for=198.51.100.$i" \
    "$admin/api/admin/v1/auth/session")"
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
# Other staff routes are not sign-ins and stay unlimited.
[[ "$(call --request POST "$admin/api/admin/v1/products")" == 401 ]] || fail "staff API limited by the sign-in zone"
echo "staff auth limit passed: $allowed passed, $limited rejected; customer API paths are 404 on the admin host"

#!/usr/bin/env bash
set -euo pipefail

# Staff sign-in rate limit through the real admin edge (ADR 008, ADR 014).
# Exhausts this client's staff sign-in bucket (recovery takes about a minute),
# so run it only in CI or another disposable environment, after recreating
# admin-edge for a fresh zone.
#   ADMIN_ORIGIN   default http://localhost:8081 (also sent as Origin)
#   SHOP_ORIGIN    default http://127.0.0.1:8080
admin="${ADMIN_ORIGIN:-http://localhost:8081}"
shop="${SHOP_ORIGIN:-http://127.0.0.1:8080}"
# Must match deploy/nginx/admin.conf.template: staff_auth 5r/m, burst 3.
burst=3
retry_after=12
attempts=12

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

fail() {
  echo "admin auth rate limit: $1" >&2
  exit 1
}

call() {
  local name="$1" method="$2" url="$3"
  shift 3
  status="$(curl --silent --show-error --path-as-is --max-time 15 --request "$method" \
    --dump-header "$temporary_dir/$name.headers" --output "$temporary_dir/$name.body" \
    --write-out '%{http_code}' "$@" "$url")"
}
has_header() { grep -qi "^$2:" "$temporary_dir/$1.headers"; }
wrong_login() {
  local name="$1"
  shift
  call "$name" POST "$admin/api/admin/v1/auth/session" --header "Origin: $admin" --header 'Content-Type: application/json' \
    --data '{"email":"nobody@example.com","password":"wrong password, not a secret"}' "$@"
}

for i in 1 2 3; do
  call get-before GET "$admin/api/admin/v1/auth/session"
  [[ "$status" == 200 ]] || fail "GET session before the burst: HTTP $status"
done

allowed=0
limited=0
for ((i = 1; i <= attempts; i++)); do
  case $((i % 3)) in
    0) spoof=(--header "X-Forwarded-For: 203.0.113.$i, 198.51.100.$i") ;;
    1) spoof=(--header "X-Real-IP: 192.0.2.$i") ;;
    2) spoof=(--header "Forwarded: for=\"[2001:db8::$i]\";proto=https") ;;
  esac
  wrong_login "burst-$i" "${spoof[@]}"
  case "$status" in
    401)
      ((allowed += 1))
      ((limited == 0)) || fail "attempt $i reached Go after the limiter engaged"
      has_header "burst-$i" X-Request-ID || fail "attempt $i: 401 without Go's X-Request-ID"
      ;;
    429)
      ((limited += 1))
      has_header "burst-$i" X-Request-ID && fail "attempt $i: 429 came from Go, not the edge"
      grep -Eqi "^retry-after:[[:space:]]*$retry_after[[:space:]]*$" "$temporary_dir/burst-$i.headers" || fail "attempt $i: Retry-After $retry_after missing"
      grep -qF 'Too many sign-in attempts' "$temporary_dir/burst-$i.body" || fail "attempt $i: unexpected 429 body"
      ;;
    *) fail "attempt $i: unexpected HTTP $status" ;;
  esac
done
((allowed >= burst + 1 && allowed <= burst + 2)) || fail "$allowed attempts reached Go, expected $((burst + 1))-$((burst + 2))"
((limited == attempts - allowed)) || fail "only $limited of the remaining attempts were limited"

for header in "X-Forwarded-For: 198.51.100.200" "X-Real-IP: 198.51.100.201" "Forwarded: for=198.51.100.202"; do
  wrong_login spoof --header "$header"
  [[ "$status" == 429 ]] || fail "spoofed '$header' bypassed the limiter: HTTP $status"
done
call get-limited GET "$admin/api/admin/v1/auth/session"
[[ "$status" == 200 ]] || fail "GET session while limited: HTTP $status"
call delete-limited DELETE "$admin/api/admin/v1/auth/session" --header "Origin: $admin"
[[ "$status" == 204 ]] || fail "DELETE session while limited: HTTP $status"
call products-limited GET "$admin/api/admin/v1/products"
[[ "$status" == 401 ]] || fail "staff API limited by the sign-in zone: HTTP $status"
call shop-admin POST "$shop/api/admin/v1/auth/session" --header 'Content-Type: application/json' --data '{}'
[[ "$status" == 404 ]] || fail "shop host /api/admin: HTTP $status"
echo "admin auth rate limit passed: $allowed reached Go, $limited rejected at the admin edge; spoofed forwarding headers did not open new buckets"

#!/usr/bin/env bash
set -euo pipefail

# Customer sign-in rate-limit smoke through the public edge.
#
# It deliberately exhausts this client's sign-in bucket (recovery takes about a
# minute) and signs in once with the smoke account, so run it only against CI
# or another disposable environment, never against production. It expects a
# fresh limiter zone: recreate the edge container first (earlier sign-ins from
# this client, such as the release flow, would otherwise use up part of the
# burst).
#   EDGE_ORIGIN    default http://127.0.0.1:8080 (also sent as Origin)
#   SMOKE_EMAIL / SMOKE_PASSWORD   default: the demo account (APP_MODE=demo)
edge_origin="${EDGE_ORIGIN:-http://127.0.0.1:8080}"
email="${SMOKE_EMAIL:-demo@sugar.ru}"
password="${SMOKE_PASSWORD:-Sweet2026!}"
# Must match deploy/nginx/*.conf.template: customer_auth rate=10r/m, burst=10.
burst=10
retry_after=6
attempts=30

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

fail() {
  echo "auth rate limit: $1" >&2
  exit 1
}

# request NAME METHOD PATH [curl args...]; sets $status.
request() {
  local name="$1" method="$2" path="$3"
  shift 3
  status="$(curl --silent --show-error --path-as-is --max-time 15 --request "$method" \
    --dump-header "$temporary_dir/$name.headers" --output "$temporary_dir/$name.body" \
    --write-out '%{http_code}' "$@" "$edge_origin$path")"
}

has_header() {
  grep -qi "^$2:" "$temporary_dir/$1.headers"
}

wrong_login() {
  local name="$1" path="$2"
  shift 2
  request "$name" POST "$path" --header "Origin: $edge_origin" --header 'Content-Type: application/json' \
    --data "{\"email\":\"$email\",\"password\":\"wrong password, not the smoke secret\"}" "$@"
}

# 1. Reading and ending a session are never limited.
for i in 1 2 3; do
  request "get-before-$i" GET /api/auth/session
  [[ "$status" == 200 ]] || fail "GET /api/auth/session before the burst: HTTP $status"
done

# 2. A burst of wrong passwords from one client. Every request carries a new
#    spoofed forwarding identity and the paths vary; all share one bucket.
paths=(/api/auth/session /api/auth/login /%61pi/auth/session //api//auth/session)
allowed=0
limited=0
first_limited=0
for ((i = 1; i <= attempts; i++)); do
  path="${paths[$((i % ${#paths[@]}))]}"
  case $((i % 3)) in
    0) spoof=(--header "X-Forwarded-For: 203.0.113.$i, 198.51.100.$i") ;;
    1) spoof=(--header "X-Real-IP: 192.0.2.$i") ;;
    2) spoof=(--header "Forwarded: for=\"[2001:db8::$i]\";proto=https") ;;
  esac
  wrong_login "burst-$i" "$path" "${spoof[@]}"
  case "$status" in
    401 | 301)
      # 301: nginx counted the merged-slash spelling, then Go redirected the
      # raw path to the clean one without checking a password.
      [[ "$status" == 401 || "$path" == //* ]] || fail "attempt $i ($path): unexpected redirect"
      ((allowed += 1))
      ((limited == 0)) || fail "attempt $i reached Go after the limiter engaged"
      has_header "burst-$i" X-Request-ID || fail "attempt $i: HTTP $status without Go's X-Request-ID"
      ;;
    429)
      ((limited += 1))
      ((first_limited)) || first_limited=$i
      has_header "burst-$i" X-Request-ID && fail "attempt $i: 429 came from Go, not the edge"
      grep -Eqi "^retry-after:[[:space:]]*$retry_after[[:space:]]*$" "$temporary_dir/burst-$i.headers" || fail "attempt $i: Retry-After $retry_after missing"
      grep -qF 'Too many sign-in attempts' "$temporary_dir/burst-$i.body" || fail "attempt $i: unexpected 429 body"
      ;;
    *) fail "attempt $i ($path): unexpected HTTP $status" ;;
  esac
done
# burst+1 pass at once; one more token may refill if the loop is slow.
((allowed >= burst + 1 && allowed <= burst + 2)) || fail "$allowed attempts reached Go, expected $((burst + 1))-$((burst + 2))"
((limited == attempts - allowed)) || fail "only $limited of the remaining attempts were limited"
echo "burst: $allowed attempts reached Go, $limited rejected at the edge from attempt $first_limited; spoofed forwarding headers did not open new buckets"

# 3. While limited: each spoofing header alone is still the same client, and
#    session reads and logout keep working.
for header in "X-Forwarded-For: 198.51.100.200" "X-Real-IP: 198.51.100.201" "Forwarded: for=198.51.100.202" "X-Forwarded-For: unknown"; do
  wrong_login spoof /api/auth/login --header "$header"
  [[ "$status" == 429 ]] || fail "spoofed '$header' bypassed the limiter: HTTP $status"
done
request get-limited GET /api/auth/session
[[ "$status" == 200 ]] || fail "GET /api/auth/session while limited: HTTP $status"
request delete-limited DELETE /api/auth/session --header "Origin: $edge_origin"
[[ "$status" == 204 ]] || fail "DELETE /api/auth/session while limited: HTTP $status"

# 4. One token refills after Retry-After; a normal sign-in, session read and
#    sign-out then work.
sleep "$((retry_after + 1))"
request login POST /api/auth/session --header "Origin: $edge_origin" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$email\",\"password\":\"$password\"}"
[[ "$status" == 200 ]] || fail "sign-in after the refill: HTTP $status"
cookie="$(grep -io '^set-cookie: [^=]*sweetshop_session=[^;]*' "$temporary_dir/login.headers" | sed 's/^[^:]*: //' | head -1)"
csrf="$(grep -o '"csrfToken":"[^"]*"' "$temporary_dir/login.body" | sed 's/.*:"//; s/"$//')"
[[ -n "$cookie" && -n "$csrf" ]] || fail "sign-in returned no session cookie or CSRF token"
request session GET /api/auth/session --header "Cookie: $cookie"
[[ "$status" == 200 ]] && grep -q '"authenticated":true' "$temporary_dir/session.body" || fail "session read after sign-in"
request logout DELETE /api/auth/session --header "Cookie: $cookie" --header "Origin: $edge_origin" --header "X-CSRF-Token: $csrf"
[[ "$status" == 204 ]] || fail "sign-out: HTTP $status"
echo "auth rate limit smoke passed for $edge_origin"

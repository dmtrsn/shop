#!/usr/bin/env bash
set -euo pipefail

# Self-service (registration and password recovery) rate-limit smoke through
# the public edge.
#
# It exhausts this client's registration, email, reset-token and sign-in
# buckets, so run it only against CI or another disposable environment, on a
# freshly created edge container (fresh limiter zones). Requests carry bodies
# that Go rejects before any password work or email (410 or 422, or 404 where
# self-service is not enabled), so the script creates nothing.
#   EDGE_ORIGIN    default http://127.0.0.1:8080 (also sent as Origin)
#   SMOKE_EMAIL / SMOKE_PASSWORD   default: the demo account (APP_MODE=demo)
edge_origin="${EDGE_ORIGIN:-http://127.0.0.1:8080}"
email="${SMOKE_EMAIL:-demo@sugar.ru}"
password="${SMOKE_PASSWORD:-Sweet2026!}"
# Must match deploy/nginx/*.conf.template.
signup_burst=5
email_burst=3
token_burst=6
auth_burst=10
registration_retry_after=20
token_retry_after=10
auth_retry_after=6

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

fail() {
  echo "self-service rate limit: $1" >&2
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

post() {
  local name="$1" path="$2"
  shift 2
  request "$name" POST "$path" --header "Origin: $edge_origin" --header 'Content-Type: application/json' --data '{}' "$@"
}

spoof_for() {
  case $(($1 % 3)) in
    0) spoof=(--header "X-Forwarded-For: 203.0.113.$1, 198.51.100.$1") ;;
    1) spoof=(--header "X-Real-IP: 192.0.2.$1") ;;
    2) spoof=(--header "Forwarded: for=\"[2001:db8::$1]\";proto=https") ;;
  esac
}

# exhaust ZONE PATH BURST RETRY_AFTER BODY_TEXT: burst+1 reach Go (one more
# may refill on a slow runner), then every request is a 429 from the edge,
# whatever forwarding header it carries.
exhaust() {
  local zone="$1" path="$2" burst="$3" retry_after="$4" text="$5" attempts=$(($3 + 8))
  local allowed=0 limited=0
  for ((i = 1; i <= attempts; i++)); do
    spoof_for "$i"
    post "$zone-$i" "$path" "${spoof[@]}"
    case "$status" in
      404 | 410 | 422)
        ((limited == 0)) || fail "$zone attempt $i reached Go after the limiter engaged"
        has_header "$zone-$i" X-Request-ID || fail "$zone attempt $i: HTTP $status without Go's X-Request-ID"
        ((allowed += 1))
        ;;
      429)
        ((limited += 1))
        has_header "$zone-$i" X-Request-ID && fail "$zone attempt $i: 429 came from Go, not the edge"
        grep -Eqi "^retry-after:[[:space:]]*$retry_after[[:space:]]*$" "$temporary_dir/$zone-$i.headers" || fail "$zone attempt $i: Retry-After $retry_after missing"
        grep -qF "$text" "$temporary_dir/$zone-$i.body" || fail "$zone attempt $i: unexpected 429 body"
        ;;
      *) fail "$zone attempt $i ($path): unexpected HTTP $status" ;;
    esac
  done
  ((allowed >= burst + 1 && allowed <= burst + 2)) || fail "$zone: $allowed attempts reached Go, expected $((burst + 1))-$((burst + 2))"
  ((limited == attempts - allowed)) || fail "$zone: only $limited of the remaining attempts were limited"
  echo "$zone: $allowed requests reached Go, $limited rejected at the edge; spoofed forwarding headers opened no new bucket"
}

# 1. Reading the session is never limited.
request get-before GET /api/auth/session
[[ "$status" == 200 ]] || fail "GET /api/auth/session: HTTP $status"

# 2. Registration, the email-sending requests and reset confirmation have
#    their own buckets.
exhaust customer_signup /api/auth/registration "$signup_burst" "$registration_retry_after" 'Too many registration requests'
exhaust customer_email /api/auth/registration/resend "$email_burst" "$registration_retry_after" 'Too many email requests'
# A password-reset request shares the resend bucket, so it is limited now.
post reset-request-limited /api/auth/password-reset --header 'X-Real-IP: 192.0.2.250'
[[ "$status" == 429 ]] || fail "password-reset request did not share the email bucket: HTTP $status"
has_header reset-request-limited X-Request-ID && fail "password-reset request 429 came from Go"
grep -qF 'Too many email requests' "$temporary_dir/reset-request-limited.body" || fail "password-reset request: unexpected 429 body"
echo "customer_email: resend and password-reset requests share one bucket"
exhaust customer_token /api/auth/password-reset/confirm "$token_burst" "$token_retry_after" 'Too many attempts'

# 3. Exhausting those buckets left sign-in alone: one wrong-password
#    sign-in still reaches Go.
request signin-free POST /api/auth/session --header "Origin: $edge_origin" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$email\",\"password\":\"wrong password, not the smoke secret\"}"
[[ "$status" == 401 ]] || fail "sign-in after registration limits: HTTP $status"

# 4. Confirmation shares the sign-in bucket: after the remaining burst is
#    spent on confirmations, sign-in is limited too.
allowed=0
for ((i = 1; i <= auth_burst + 6; i++)); do
  spoof_for "$i"
  post "confirm-$i" /api/auth/registration/confirm "${spoof[@]}"
  case "$status" in
    404 | 410 | 422) ((allowed += 1)) ;;
    429) has_header "confirm-$i" X-Request-ID && fail "confirm attempt $i: 429 came from Go" ;;
    *) fail "confirm attempt $i: unexpected HTTP $status" ;;
  esac
done
# The sign-in above used one token of the shared burst.
((allowed >= auth_burst && allowed <= auth_burst + 1)) || fail "confirm: $allowed reached Go, expected $auth_burst-$((auth_burst + 1))"
grep -Eqi "^retry-after:[[:space:]]*$auth_retry_after[[:space:]]*$" "$temporary_dir/confirm-$((auth_burst + 6)).headers" || fail "confirm: Retry-After $auth_retry_after missing"
request signin-limited POST /api/auth/session --header "Origin: $edge_origin" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$email\",\"password\":\"wrong password, not the smoke secret\"}" --header 'X-Forwarded-For: 198.51.100.250'
[[ "$status" == 429 ]] || fail "sign-in was not limited by confirmations: HTTP $status"
echo "customer_auth: confirmations and sign-ins share one bucket"

# 5. While every bucket is spent, session reads and logout still work.
request get-limited GET /api/auth/session
[[ "$status" == 200 ]] || fail "GET /api/auth/session while limited: HTTP $status"
request delete-limited DELETE /api/auth/session --header "Origin: $edge_origin"
[[ "$status" == 204 ]] || fail "DELETE /api/auth/session while limited: HTTP $status"

# 6. Buckets refill: a sign-in after Retry-After, and a registration-shaped
#    request after its Retry-After, reach Go again.
sleep "$((auth_retry_after + 1))"
request login POST /api/auth/session --header "Origin: $edge_origin" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$email\",\"password\":\"$password\"}"
[[ "$status" == 200 ]] || fail "sign-in after the refill: HTTP $status"
sleep "$((registration_retry_after - auth_retry_after))"
post refill /api/auth/registration
[[ "$status" == 404 || "$status" == 422 ]] || fail "registration after the refill: HTTP $status"
post reset-refill /api/auth/password-reset/confirm
[[ "$status" == 404 || "$status" == 410 ]] || fail "password-reset confirmation after the refill: HTTP $status"
# The staff API is still not served from the shop host.
request admin POST /api/admin/v1/auth/session --header "Origin: $edge_origin"
[[ "$status" == 404 ]] || fail "/api/admin on the shop host: HTTP $status"
echo "self-service rate limit smoke passed for $edge_origin"

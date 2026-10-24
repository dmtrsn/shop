#!/usr/bin/env bash
set -euo pipefail

# Admin host smoke against the real Compose stack (ADR 014). Needs the
# "admin" profile, STAFF_TRUSTED_ORIGINS=$ADMIN_ORIGIN on app, a staff
# account provisioned with shopctl, and SESSION_COOKIE_SECURE=true for the
# production cookie checks. Disposable environments only: it signs in, uses
# two staff sign-in attempts of the admin edge bucket, and signs out.
#   ADMIN_ORIGIN   default http://localhost:8081 (sent as Origin)
#   SHOP_ORIGIN    default http://127.0.0.1:8080
#   ADMIN_SMOKE_EMAIL / ADMIN_SMOKE_PASSWORD   the provisioned staff account
#   CUSTOMER_EMAIL / CUSTOMER_PASSWORD         default: the demo customer
admin="${ADMIN_ORIGIN:-http://localhost:8081}"
shop="${SHOP_ORIGIN:-http://127.0.0.1:8080}"
email="${ADMIN_SMOKE_EMAIL:?ADMIN_SMOKE_EMAIL is required}"
password="${ADMIN_SMOKE_PASSWORD:?ADMIN_SMOKE_PASSWORD is required}"
customer_email="${CUSTOMER_EMAIL:-demo@sugar.ru}"
customer_password="${CUSTOMER_PASSWORD:-Sweet2026!}"

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

fail() {
  echo "admin edge: $1" >&2
  exit 1
}

# call NAME METHOD URL [curl args...]; sets $status. Never follows redirects.
call() {
  local name="$1" method="$2" url="$3"
  shift 3
  status="$(curl --silent --show-error --path-as-is --max-time 15 --request "$method" \
    --dump-header "$temporary_dir/$name.headers" --output "$temporary_dir/$name.body" \
    --write-out '%{http_code}' "$@" "$url")"
}
header() { grep -i "^$2:" "$temporary_dir/$1.headers" | head -1 | sed 's/^[^:]*:[[:space:]]*//; s/\r$//'; }
expect() { [[ "$status" == "$1" ]] || fail "$2: expected HTTP $1, got $status ($(head -c 200 "$temporary_dir/$3.body" 2>/dev/null))"; }
from_go() { [[ -n "$(header "$1" X-Request-ID)" ]]; }
json() { grep -q "$2" "$temporary_dir/$1.body" || fail "$1: body lacks $2"; }

# 1. The admin Next app owns pages, with its own strict headers.
call login GET "$admin/login"
expect 200 "admin /login" login
[[ "$(header login X-Admin-Runtime)" == next ]] || fail "/login not served by the admin app"
[[ -z "$(header login X-Storefront-Runtime)" ]] || fail "the storefront answered on the admin host"
csp="$(header login Content-Security-Policy)"
[[ "$csp" == *"'nonce-"* && "$csp" == *"frame-ancestors 'none'"* && "$csp" != *unsafe* ]] || fail "admin CSP: $csp"
[[ "$(header login Cache-Control)" == *no-store* ]] || fail "admin page is cacheable"
[[ "$(header login X-Robots-Tag)" == *noindex* ]] || fail "admin page is indexable"
json login 'Вход для сотрудников'
call root GET "$admin/"
[[ "$status" == 307 || "$status" == 303 ]] && [[ "$(header root Location)" == */login ]] || fail "anonymous / did not redirect to /login: $status"
for path in /legacy /assets/product-chocolate.webp /catalog; do
  call page GET "$admin$path"
  expect 404 "admin $path" page
  [[ "$(header page X-Admin-Runtime)" == next ]] || fail "$path not answered by the admin app"
done

# 2. The customer API does not exist on the admin host.
for path in /api /api/health /api/catalog /api/auth/session /api/me /api/products/p01 /%61pi/catalog //api//catalog /api/admin; do
  for method in GET POST; do
    call customer-api "$method" "$admin$path" --header "Origin: $admin"
    expect 404 "admin host $method $path" customer-api
    from_go customer-api && fail "admin host $method $path reached Go"
  done
done

# 3. The staff API reaches Go and keeps its rules.
call state GET "$admin/api/admin/v1/auth/session"
expect 200 "staff session state" state
from_go state || fail "staff session state did not come from Go"
json state '"authenticated":false'
call products GET "$admin/api/admin/v1/products"
expect 401 "anonymous product list" products
call create POST "$admin/api/admin/v1/products" --header "Origin: $admin" --header 'Content-Type: application/json' --data '{}'
expect 401 "anonymous product create" create
call shop-origin POST "$admin/api/admin/v1/auth/session" --header "Origin: $shop" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$email\",\"password\":\"$password\"}"
expect 403 "staff sign-in from the shop origin" shop-origin
json shop-origin '"code":"csrf_failed"'
call wrong POST "$admin/api/admin/v1/auth/session" --header "Origin: $admin" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$email\",\"password\":\"wrong password, not the smoke secret\"}"
expect 401 "wrong staff password" wrong
json wrong '"code":"unauthorized"'
call anonymous-logout DELETE "$admin/api/admin/v1/auth/session" --header "Origin: $admin"
expect 204 "anonymous staff sign-out" anonymous-logout

# 4. Sign-in sets the production staff cookie, host-only on the admin host.
call staff-login POST "$admin/api/admin/v1/auth/session" --header "Origin: $admin" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$email\",\"password\":\"$password\"}"
expect 200 "staff sign-in" staff-login
set_cookie="$(grep -i '^set-cookie: __Host-sweetshop_staff=' "$temporary_dir/staff-login.headers" | head -1 | tr -d '\r')"
[[ -n "$set_cookie" ]] || fail "no __Host-sweetshop_staff cookie (is SESSION_COOKIE_SECURE=true?)"
attributes="${set_cookie,,}"
for attribute in '; secure' '; httponly' '; samesite=strict' '; path=/'; do
  [[ "$attributes" == *"$attribute"* ]] || fail "staff cookie lacks ${attribute#; }: $set_cookie"
done
[[ "$attributes" != *'; domain='* ]] || fail "staff cookie has a Domain: $set_cookie"
token="$(sed 's/^[^=]*=//; s/;.*//' <<<"${set_cookie#*: }")"
grep -qF "$token" "$temporary_dir/staff-login.body" && fail "staff token in the response body"
csrf="$(grep -o '"csrfToken":"[^"]*"' "$temporary_dir/staff-login.body" | sed 's/.*:"//; s/"$//')"
staff_id="$(grep -o '"id":"stf_[0-9a-f]*"' "$temporary_dir/staff-login.body" | head -1 | sed 's/.*:"//; s/"$//')"
[[ -n "$csrf" && -n "$staff_id" ]] || fail "sign-in body lacks csrfToken or staff id"
staff_cookie="__Host-sweetshop_staff=$token"

call me GET "$admin/api/admin/v1/auth/session" --header "Cookie: $staff_cookie"
json me '"authenticated":true'
call list GET "$admin/api/admin/v1/products?limit=1" --header "Cookie: $staff_cookie"
expect 200 "staff product list through the admin host" list
call page-auth GET "$admin/" --header "Cookie: $staff_cookie"
expect 200 "authenticated admin page" page-auth
json page-auth 'Панель управления'
grep -qF "$csrf" "$temporary_dir/page-auth.body" && fail "the CSRF token was rendered into HTML"

# 5. Principals never cross: the staff cookie is not a customer session, and a
#    customer session is not a staff session.
call shop-me GET "$shop/api/me" --header "Cookie: $staff_cookie"
expect 401 "staff cookie on the customer API" shop-me
call shop-login POST "$shop/api/auth/session" --header "Origin: $shop" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$customer_email\",\"password\":\"$customer_password\"}"
expect 200 "customer sign-in on the shop host" shop-login
customer_cookie="$(grep -io '^set-cookie: [^=]*sweetshop_session=[^;]*' "$temporary_dir/shop-login.headers" | sed 's/^[^:]*: //' | head -1)"
[[ -n "$customer_cookie" ]] || fail "customer sign-in set no cookie"
call customer-on-admin GET "$admin/api/admin/v1/auth/session" --header "Cookie: $customer_cookie"
json customer-on-admin '"authenticated":false'
call customer-products GET "$admin/api/admin/v1/products" --header "Cookie: $customer_cookie"
expect 401 "customer cookie on staff products" customer-products
call admin-origin-on-shop POST "$shop/api/auth/session" --header "Origin: $admin" --header 'Content-Type: application/json' \
  --data "{\"email\":\"$customer_email\",\"password\":\"$customer_password\"}"
expect 403 "admin origin on the customer sign-in" admin-origin-on-shop

# 6. The shop host still has no admin API, for any staff route or spelling.
for method in GET POST PUT DELETE; do
  for path in /api/admin/v1/auth/session /api/admin/v1/products /api/admin/v1/products/p01 /api/admin/v1/products/p01/archive /%61pi/admin/v1/products //api//admin/v1/products; do
    call shop-admin "$method" "$shop$path" --header "Cookie: $staff_cookie" --header "Origin: $admin" --header 'Content-Type: application/json' --data '{}'
    expect 404 "shop host $method $path" shop-admin
    from_go shop-admin && fail "shop host $method $path reached Go"
  done
done

# 7. Sign-out needs the staff CSRF token and the expected staff ID.
call logout-other DELETE "$admin/api/admin/v1/auth/session" --header "Cookie: $staff_cookie" --header "Origin: $admin" \
  --header "X-CSRF-Token: $csrf" --header "X-Expected-Staff-ID: stf_00000000000000000000000000000000"
expect 409 "sign-out of another staff member" logout-other
call logout-csrf DELETE "$admin/api/admin/v1/auth/session" --header "Cookie: $staff_cookie" --header "Origin: $admin" --header "X-Expected-Staff-ID: $staff_id"
expect 403 "sign-out without CSRF" logout-csrf
call logout DELETE "$admin/api/admin/v1/auth/session" --header "Cookie: $staff_cookie" --header "Origin: $admin" \
  --header "X-CSRF-Token: $csrf" --header "X-Expected-Staff-ID: $staff_id"
expect 204 "staff sign-out" logout
call after GET "$admin/api/admin/v1/products" --header "Cookie: $staff_cookie"
expect 401 "revoked staff session" after
echo "admin edge smoke passed for $admin (shop $shop)"

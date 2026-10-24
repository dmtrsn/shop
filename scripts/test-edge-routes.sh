#!/usr/bin/env bash
set -euo pipefail

# Public edge route-ownership smoke.
#   EXPECT_ROOT_OWNER=next    (default) final cutover: Next owns / and unknown paths
#   EXPECT_ROOT_OWNER=legacy  rollback map (EDGE_ROUTE_MAP=legacy-root): Go owns them
# /api, /assets and /legacy are Go-owned in both modes.
edge_origin="${EDGE_ORIGIN:-http://127.0.0.1:8080}"
root_owner="${EXPECT_ROOT_OWNER:-next}"
# Seeded fixture (memory store and PostgreSQL demo seed); override for other data.
product_slug="${SMOKE_PRODUCT_SLUG:-dark-chocolate-sea-salt}"
product_name="${SMOKE_PRODUCT_NAME:-Тёмный шоколад 70% с морской солью}"

case "$root_owner" in
  next | legacy) ;;
  *)
    echo "EXPECT_ROOT_OWNER must be next or legacy, got: $root_owner" >&2
    exit 2
    ;;
esac

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

fail() {
  echo "$1" >&2
  exit 1
}

request() {
  local name="$1"
  local path="$2"
  status="$(curl --silent --show-error --location --path-as-is --max-time 15 \
    --dump-header "$temporary_dir/$name.headers" \
    --output "$temporary_dir/$name.body" \
    --write-out '%{http_code}' "$edge_origin$path")"
}

# Does not follow redirects, so the redirect itself can be inspected.
request_raw() {
  local name="$1"
  local path="$2"
  status="$(curl --silent --show-error --path-as-is --max-time 15 \
    --dump-header "$temporary_dir/$name.headers" \
    --output "$temporary_dir/$name.body" \
    --write-out '%{http_code}' "$edge_origin$path")"
}

expect_status() {
  local expected="$1"
  local actual="$2"
  local name="$3"
  [[ "$actual" == "$expected" ]] || fail "$name: expected HTTP $expected, got $actual"
}

is_next() {
  grep -qi '^x-storefront-runtime: next' "$temporary_dir/$1.headers"
}

expect_next() {
  is_next "$1" || fail "$1: response was not owned by Next"
}

expect_go() {
  ! is_next "$1" || fail "$1: unexpectedly owned by Next"
}

expect_legacy() {
  expect_go "$1"
  grep -q 'id="app-main"' "$temporary_dir/$1.body" || fail "$1: not the legacy storefront shell"
}

expect_body() {
  grep -qF -- "$2" "$temporary_dir/$1.body" || fail "$1: body lacks: $2"
}

expect_next_html() {
  expect_next "$1"
  expect_body "$1" '<html lang="ru"'
  if grep -q 'id="app-main"' "$temporary_dir/$1.body"; then
    fail "$1: Next page contains the legacy shell"
  fi
}

# Internal service names and ports must never reach browser-visible output.
expect_no_internal_hosts() {
  if grep -Eqi '(^|[^a-z0-9.-])(app|storefront):[0-9]+|:3000([^0-9]|$)' "$@"; then
    fail "internal service address leaked in: $*"
  fi
}

expect_next_security_headers() {
  local name="$1"
  local headers="$temporary_dir/$name.headers"

  [[ "$(grep -ci '^content-security-policy:' "$headers")" == 1 ]] || fail "$name: expected exactly one CSP header"
  grep -Eqi "^content-security-policy:.*script-src[^;]*'nonce-[^']+'" "$headers" || fail "$name: CSP lacks a script nonce"
  grep -Eqi "^content-security-policy:.*script-src[^;]*'strict-dynamic'" "$headers" || fail "$name: CSP lacks strict-dynamic"
  grep -Eqi "^content-security-policy:.*frame-ancestors 'none'" "$headers" || fail "$name: CSP lacks frame-ancestors"
  grep -Eqi '^x-frame-options:[[:space:]]*DENY[[:space:]]*$' "$headers" || fail "$name: X-Frame-Options"
  grep -Eqi '^x-content-type-options:[[:space:]]*nosniff[[:space:]]*$' "$headers" || fail "$name: X-Content-Type-Options"
  grep -Eqi '^referrer-policy:[[:space:]]*strict-origin-when-cross-origin[[:space:]]*$' "$headers" || fail "$name: Referrer-Policy"
  grep -Eqi '^permissions-policy:' "$headers" || fail "$name: Permissions-Policy"
  grep -Eqi '^cross-origin-opener-policy:[[:space:]]*same-origin' "$headers" || fail "$name: Cross-Origin-Opener-Policy"
}

# --- Edge and Go-owned paths: identical in both modes ---------------------

request edge-health /edge/healthz
expect_status 200 "$status" edge-health
expect_body edge-health '"status":"ok"'

request api-health /api/health
expect_status 200 "$status" api-health
expect_go api-health
expect_body api-health '"status":"ok"'

# The staff (admin) API never answers on the shop host, whatever Go's
# STAFF_TRUSTED_ORIGINS is: the edge itself returns 404 (ADR 006).
for method in GET POST DELETE; do
  for path in /api/admin /api/admin/v1/auth/session /%61pi/admin/v1/auth/session //api//admin/v1/auth/session /api/admin/v1/products /api/admin/v1/products/p01/archive; do
    status="$(curl --silent --show-error --path-as-is --max-time 15 --request "$method" \
      --header 'Origin: https://admin.invalid' --header 'Content-Type: application/json' --data '{}' \
      --dump-header "$temporary_dir/admin.headers" --output "$temporary_dir/admin.body" \
      --write-out '%{http_code}' "$edge_origin$path")"
    expect_status 404 "$status" "admin-blocked $method $path"
    if grep -qi '^set-cookie:.*sweetshop_staff' "$temporary_dir/admin.headers"; then
      fail "admin-blocked $method $path: staff cookie set"
    fi
  done
done

# Nginx matches the decoded path, so an encoded /api still reaches Go.
request api-encoded /%61pi/health
expect_status 200 "$status" api-encoded
expect_go api-encoded
expect_body api-encoded '"status":"ok"'

request legacy-asset /assets/product-chocolate.webp
expect_status 200 "$status" legacy-asset
expect_go legacy-asset
test -s "$temporary_dir/legacy-asset.body" || fail "legacy-asset: empty body"

# Fragments never reach HTTP. Test the login pathname/query here;
# browser tests cover #/login and post-login navigation.
for path in /legacy /legacy/ '/legacy/?returnTo=%2Fcheckout' /legacy/migration-owner-check; do
  request legacy-entry "$path"
  expect_status 200 "$status" legacy-entry
  expect_legacy legacy-entry
  grep -Eqi '^x-frame-options:[[:space:]]*DENY[[:space:]]*$' "$temporary_dir/legacy-entry.headers" \
    || fail "legacy-entry $path: X-Frame-Options"
done
for file in app.js migration.js styles.css; do
  request legacy-shell "/$file"
  expect_status 200 "$status" legacy-shell
  expect_go legacy-shell
  test -s "$temporary_dir/legacy-shell.body" || fail "$file: empty body"
  if grep -qi '<!doctype html' "$temporary_dir/legacy-shell.body"; then
    fail "$file: legacy shell asset was swallowed by an HTML fallback"
  fi
  request legacy-prefixed-shell "/legacy/$file"
  expect_status 200 "$status" legacy-prefixed-shell
  cmp -s "$temporary_dir/legacy-shell.body" "$temporary_dir/legacy-prefixed-shell.body" \
    || fail "$file: /$file and /legacy/$file differ"
done

# --- Root ownership --------------------------------------------------------

if [[ "$root_owner" == legacy ]]; then
  for path in / /migration-owner-check /catalog /storefront-preview; do
    request legacy-root "$path"
    expect_status 200 "$status" legacy-root
    expect_legacy legacy-root
  done
  echo "edge route ownership smoke test passed for $edge_origin (root owner: legacy rollback)"
  exit 0
fi

request next-home /
expect_status 200 "$status" next-home
expect_next_html next-home
expect_next_security_headers next-home
expect_no_internal_hosts "$temporary_dir/next-home.headers" "$temporary_dir/next-home.body"
if grep -q '/storefront-preview' "$temporary_dir/next-home.body"; then
  fail "next-home: root artifact still references the preview base path"
fi

next_asset="$(grep -oE '/_next/static/[^"[:space:]]+\.js' "$temporary_dir/next-home.body" | head -n 1 || true)"
[[ -n "$next_asset" ]] || fail 'next-home: did not expose a root /_next static asset'
request next-asset "$next_asset"
expect_status 200 "$status" next-asset
expect_next next-asset
test -s "$temporary_dir/next-asset.body" || fail "next-asset: empty body"

request next-health /healthz
expect_status 200 "$status" next-health
expect_next next-health
expect_body next-health '"status":"ok"'

request next-catalog /catalog
expect_status 200 "$status" next-catalog
expect_next_html next-catalog
expect_next_security_headers next-catalog

request next-product "/products/$product_slug"
expect_status 200 "$status" next-product
expect_next_html next-product
expect_body next-product "$product_name"

# The product route streams (loading.tsx), so Next may have committed 200 before
# notFound(); it then serves the not-found UI with robots noindex (a soft 404).
request next-product-missing /products/edge-smoke-missing-product
[[ "$status" == 404 || "$status" == 200 ]] || fail "next-product-missing: unexpected HTTP $status"
expect_next_html next-product-missing
expect_body next-product-missing 'Товар не найден'
expect_body next-product-missing '<meta name="robots" content="noindex"/>'

for path in /migration-owner-check /storefront-preview /storefront-preview/cart; do
  request next-unknown "$path"
  expect_status 404 "$status" next-unknown
  expect_next_html next-unknown
  expect_next_security_headers next-unknown
  expect_body next-unknown 'Страница не найдена'
done

# Next canonicalizes trailing slashes before proxy.ts runs, so this bodyless
# redirect has no runtime marker. Go would answer 200 with the legacy shell.
request_raw next-slash /catalog/
[[ "$status" =~ ^30[1278]$ ]] || fail "next-slash: expected a redirect, got $status"
if grep -q 'id="app-main"' "$temporary_dir/next-slash.body"; then
  fail "next-slash: answered by the legacy shell"
fi
location="$(grep -i '^location:' "$temporary_dir/next-slash.headers" | tr -d '\r' | cut -d' ' -f2-)"
[[ "$location" == /catalog || "$location" == "$edge_origin/catalog" ]] \
  || fail "next-slash: unexpected Location: $location"
expect_no_internal_hosts "$temporary_dir/next-slash.headers"

echo "edge route ownership smoke test passed for $edge_origin (root owner: next)"

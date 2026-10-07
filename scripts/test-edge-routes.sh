#!/usr/bin/env bash
set -euo pipefail

edge_origin="${EDGE_ORIGIN:-http://127.0.0.1:8080}"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

request() {
  local name="$1"
  local path="$2"
  status="$(curl --silent --show-error --location --max-time 15 \
    --dump-header "$temporary_dir/$name.headers" \
    --output "$temporary_dir/$name.body" \
    --write-out '%{http_code}' "$edge_origin$path")"
}

expect_status() {
  local expected="$1"
  local actual="$2"
  local name="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "$name: expected HTTP $expected, got $actual" >&2
    return 1
  fi
}

expect_next() {
  local name="$1"
  if ! grep -qi '^x-storefront-runtime: next' "$temporary_dir/$name.headers"; then
    echo "$name: response was not owned by Next" >&2
    return 1
  fi
}

expect_legacy() {
  local name="$1"
  if grep -qi '^x-storefront-runtime: next' "$temporary_dir/$name.headers"; then
    echo "$name: unexpectedly owned by Next before cutover" >&2
    return 1
  fi
  grep -q 'id="app-main"' "$temporary_dir/$name.body"
}

expect_preview_security_headers() {
  local name="$1"
  local headers="$temporary_dir/$name.headers"

  if ! grep -Eqi "^content-security-policy:.*script-src[^;]*'nonce-[^']+'" "$headers"; then
    echo "$name: CSP lacks a script nonce" >&2
    return 1
  fi
  if ! grep -Eqi "^content-security-policy:.*script-src[^;]*'strict-dynamic'" "$headers"; then
    echo "$name: CSP lacks strict-dynamic" >&2
    return 1
  fi
  grep -Eqi '^x-frame-options:[[:space:]]*DENY[[:space:]]*$' "$headers"
  grep -Eqi '^x-content-type-options:[[:space:]]*nosniff[[:space:]]*$' "$headers"
  grep -Eqi '^referrer-policy:[[:space:]]*strict-origin-when-cross-origin[[:space:]]*$' "$headers"
}

request edge-health /edge/healthz
expect_status 200 "$status" edge-health
grep -q '"status":"ok"' "$temporary_dir/edge-health.body"

request api-health /api/health
expect_status 200 "$status" api-health
grep -q '"status":"ok"' "$temporary_dir/api-health.body"

request legacy-asset /assets/product-chocolate.webp
expect_status 200 "$status" legacy-asset
test -s "$temporary_dir/legacy-asset.body"

request preview-health /storefront-preview/healthz
expect_status 200 "$status" preview-health
expect_next preview-health
grep -q '"status":"ok"' "$temporary_dir/preview-health.body"

request preview-home /storefront-preview
expect_status 200 "$status" preview-home
expect_next preview-home
expect_preview_security_headers preview-home
grep -qi '<html' "$temporary_dir/preview-home.body"

next_asset="$(grep -oE '/storefront-preview/_next/static/[^"[:space:]]+\.js' "$temporary_dir/preview-home.body" | head -n 1 || true)"
if [[ -z "$next_asset" ]]; then
  echo 'preview-home: did not expose a Next static asset' >&2
  exit 1
fi
request next-asset "$next_asset"
expect_status 200 "$status" next-asset
expect_next next-asset
test -s "$temporary_dir/next-asset.body"

request legacy-home /
expect_status 200 "$status" legacy-home
expect_legacy legacy-home

request legacy-unknown /migration-owner-check
expect_status 200 "$status" legacy-unknown
expect_legacy legacy-unknown

request preview-missing /storefront-preview/migration-owner-check
expect_status 404 "$status" preview-missing
expect_next preview-missing

echo "edge route ownership smoke test passed for $edge_origin"

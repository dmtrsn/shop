#!/usr/bin/env bash
set -euo pipefail

# TEST ONLY. Runs a command with a throwaway TLS terminator in front of the
# admin edge, the production topology of ADR 014: browsers accept the
# __Host-sweetshop_staff cookie only from an https origin. The certificate is
# self-signed for localhost and lives in a temporary directory; the browser
# under test must ignore certificate errors. The terminator joins the Compose
# network of COMPOSE_PROJECT and publishes https://localhost:$ADMIN_TLS_PORT on
# loopback. Go must list that origin in STAFF_TRUSTED_ORIGINS. Requires Docker
# and openssl.
#   COMPOSE_PROJECT   default sweetshop-edge-smoke
#   ADMIN_TLS_PORT    default 8443
# Usage: scripts/with-admin-tls-proxy.sh node scripts/test-admin-browser-flow.mjs
project="${COMPOSE_PROJECT:-sweetshop-edge-smoke}"
port="${ADMIN_TLS_PORT:-8443}"
image="${NGINX_IMAGE:-nginx:1.27-alpine}"
temporary_dir="$(mktemp -d)"
container="admin-tls-proxy-$$"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$temporary_dir"
}
trap cleanup EXIT

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
  -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost" \
  -keyout "$temporary_dir/key.pem" -out "$temporary_dir/cert.pem" 2>/dev/null
chmod 644 "$temporary_dir/key.pem"
cat >"$temporary_dir/tls.conf" <<NGINX
server {
    listen 8443 ssl;
    ssl_certificate /etc/nginx/tls/cert.pem;
    ssl_certificate_key /etc/nginx/tls/key.pem;
    location / {
        proxy_http_version 1.1;
        proxy_set_header Host \$host:$port;
        proxy_set_header X-Forwarded-Proto https;
        proxy_pass http://admin-edge:8080;
    }
}
NGINX

docker run -d --name "$container" --network "${project}_default" \
  -v "$temporary_dir/tls.conf:/etc/nginx/conf.d/default.conf:ro" \
  -v "$temporary_dir:/etc/nginx/tls:ro" \
  -p "127.0.0.1:$port:8443" "$image" >/dev/null
for _ in $(seq 50); do
  curl --silent --insecure --fail "https://localhost:$port/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl --silent --insecure --fail "https://localhost:$port/healthz" >/dev/null || {
  echo "TLS proxy did not start: $(docker logs "$container" 2>&1 | tail -5)" >&2
  exit 1
}
ADMIN_ORIGIN="https://localhost:$port" CHROME_IGNORE_CERTIFICATE_ERRORS=1 "$@"

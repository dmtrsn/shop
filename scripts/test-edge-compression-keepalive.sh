#!/usr/bin/env bash
set -euo pipefail

# Exercise all production edge templates with isolated HTTP upstream stubs.
# Requires Docker, curl, gzip and Python 3. No application data is touched.
root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
container="edge-compression-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; rm -rf "$tmp"; }
trap cleanup EXIT
fail() { echo "edge compression: $*" >&2; exit 1; }

python3 - "$tmp/zz-stubs.conf" <<'PY'
import sys
body = '{"items":["' + 'catalog item ' * 150 + '"]}'
with open(sys.argv[1], 'w') as f:
    f.write('''log_format upstream_connections '$connection $request_uri';
server {
    listen 127.0.0.2:8080;
    access_log /var/log/nginx/upstream-connections.log upstream_connections;
    location /media/ { default_type image/jpeg; add_header Cache-Control "public, max-age=31536000, immutable"; add_header ETag '"fixture"'; return 200 'image fixture'; }
    location = /api/small { default_type application/json; return 200 '{"ok":true}'; }
    location = /api/precompressed { default_type application/json; add_header Content-Encoding gzip; return 200 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; }
    location / { default_type application/json; add_header X-Content-Type-Options nosniff; return 200 '%s'; }
}
server { listen 3000; location / { return 200 'storefront'; } }
server { listen 3001; location / { return 200 'admin'; } }
''' % body)
PY

for edge in default legacy-root admin; do
  docker run -d --name "$container" --add-host app:127.0.0.2 \
    --add-host storefront:127.0.0.1 --add-host admin:127.0.0.1 \
    -e STOREFRONT_PORT=3000 -e ADMIN_PORT=3001 \
    -v "$root/deploy/nginx/$edge.conf.template:/etc/nginx/templates/default.conf.template:ro" \
    -v "$tmp/zz-stubs.conf:/etc/nginx/conf.d/zz-stubs.conf:ro" \
    -p 127.0.0.1::8080 "${NGINX_IMAGE:-nginx:1.27-alpine}" >/dev/null
  origin="http://$(docker port "$container" 8080/tcp | head -1)"
  docker exec "$container" nginx -t >/dev/null
  request() { curl -sS --max-time 10 -H 'Accept-Encoding: gzip' -D "$tmp/headers" -o "$tmp/body" "$origin$1"; }
  if [[ "$edge" != admin ]]; then
    curl -sS -H 'Accept-Encoding: identity' -o "$tmp/plain" "$origin/api/catalog"
    request /api/catalog
    grep -qi '^Content-Encoding: gzip' "$tmp/headers" || fail "$edge: JSON not compressed"
    grep -qi '^Vary:.*Accept-Encoding' "$tmp/headers" || fail "$edge: missing Vary"
    gzip -dc "$tmp/body" > "$tmp/uncompressed"
    cmp -s "$tmp/plain" "$tmp/uncompressed" || fail "$edge: body differs"
    request /api/precompressed
    [[ "$(grep -ci '^Content-Encoding:' "$tmp/headers")" == 1 ]] || fail "$edge: double encoding"
    [[ "$(wc -c < "$tmp/body")" == 1500 ]] || fail "$edge: modified already encoded body"
    for path in /api/small /api/auth/session /api/auth/login /api/auth/registration /api/auth/registration/confirm /api/auth/password-reset /api/auth/password-reset/confirm /api/auth/other; do
      request "$path"
      ! grep -qi '^Content-Encoding:' "$tmp/headers" || fail "$edge: compressed $path"
    done
    request /api/admin/v1/products
    grep -q '^HTTP/1.1 404' "$tmp/headers" || fail "$edge: admin not blocked"
    route=/api/catalog
  else
    request /api/catalog
    grep -q '^HTTP/1.1 404' "$tmp/headers" || fail 'admin: customer API not blocked'
    request /api/admin/v1/products
    grep -q '^HTTP/1.1 200' "$tmp/headers" || fail 'admin: staff API not proxied'
    ! grep -qi '^Content-Encoding:' "$tmp/headers" || fail 'admin: unexpected gzip'
    route=/api/admin/v1/products
  fi
  for path in /media/example /media/example/thumbnail/hash; do
    request "$path"
    grep -qi '^Content-Type: image/jpeg' "$tmp/headers" || fail "$edge: media content type"
    grep -qi '^Cache-Control:.*immutable' "$tmp/headers" || fail "$edge: media cache"
    grep -qi '^ETag:' "$tmp/headers" || fail "$edge: media ETag"
    ! grep -qi '^Content-Encoding:' "$tmp/headers" || fail "$edge: media compressed"
    curl -fsSI -H 'Accept-Encoding: gzip' "$origin$path" > "$tmp/head"
    grep -qi '^ETag:' "$tmp/head" || fail "$edge: media HEAD"
  done
  for _ in $(seq 1 12); do curl -fsS -o /dev/null "$origin$route"; done
  docker exec "$container" cat /var/log/nginx/upstream-connections.log > "$tmp/connections"
  awk -v path="$route" '$2 == path { count[$1]++ } END { for (id in count) if (count[id] > 1) exit 0; exit 1 }' "$tmp/connections" || fail "$edge: no upstream reuse observed"
  docker rm -f "$container" >/dev/null
  echo "$edge: gzip, routes, media and upstream reuse passed"
done

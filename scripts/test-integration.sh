#!/usr/bin/env bash
# A disposable cluster; never reads DATABASE_URL or the developer's .env.
set -euo pipefail
cd "$(dirname "$0")/.."
GO=${GO:-go}
if [[ -n ${TEST_DATABASE_URL:-} ]]; then
  exec "$GO" test -tags=integration -race -count=1 -timeout=5m ./tests/integration "$@"
fi
PG_BINDIR=${PG_BINDIR:-$(pg_config --bindir 2>/dev/null || true)}
if [[ -x "$PG_BINDIR/initdb" && $(id -u) != 0 ]]; then
  cluster=$(mktemp -d /tmp/shop-pg.XXXXXX)
  cleanup() {
    "$PG_BINDIR/pg_ctl" -D "$cluster/data" -m immediate stop >/dev/null 2>&1 || true
    rm -rf "$cluster"
  }
  trap cleanup EXIT
  "$PG_BINDIR/initdb" -D "$cluster/data" -U shop_test_admin -A trust --no-locale -E UTF8 >"$cluster/init.log"
  # No TCP listener. The socket is inside a private, randomly named directory.
  "$PG_BINDIR/pg_ctl" -D "$cluster/data" -l "$cluster/server.log" -o "-F -c listen_addresses='' -k $cluster" -w start >/dev/null
  export TEST_DATABASE_URL="host=$cluster user=shop_test_admin dbname=postgres sslmode=disable"
else
  container=$(docker run --rm -d -e POSTGRES_USER=shop_test_admin -e POSTGRES_HOST_AUTH_METHOD=trust -p 127.0.0.1::5432 postgres:16-alpine)
  trap 'docker stop "$container" >/dev/null' EXIT
  ready=false
  for ((i=0; i<60; i++)); do
    if docker exec "$container" pg_isready -U shop_test_admin -d postgres >/dev/null 2>&1; then ready=true; break; fi
    sleep 1
  done
  [[ $ready == true ]] || { echo 'Temporary PostgreSQL did not become ready' >&2; exit 1; }
  port=$(docker port "$container" 5432/tcp)
  export TEST_DATABASE_URL="postgres://shop_test_admin@${port}/postgres?sslmode=disable"
fi
"$GO" test -tags=integration -race -count=1 -timeout=5m ./tests/integration "$@"

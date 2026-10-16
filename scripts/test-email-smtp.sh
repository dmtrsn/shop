#!/usr/bin/env bash
set -euo pipefail

# Real-binary SMTP check: shopctl email test against a disposable Mailpit.
# Requires Docker, curl, python3 and a built shopctl (SHOPCTL, default
# bin/shopctl from `make build`).
shopctl="${SHOPCTL:-bin/shopctl}"
image="${MAILPIT_IMAGE:-axllent/mailpit:v1.27.10}"
password="SENTINEL-SMTP-PASSWORD-$$"
container="mailpit-smtp-test-$$"
temporary_dir="$(mktemp -d)"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$temporary_dir"
}
trap cleanup EXIT

fail() {
  echo "email smtp: $1" >&2
  exit 1
}

[[ -x "$shopctl" ]] || fail "$shopctl is missing; run make build"
docker run -d --name "$container" -p 127.0.0.1::1025 -p 127.0.0.1::8025 "$image" >/dev/null
smtp_port="$(docker port "$container" 1025/tcp | head -1 | sed 's/.*://')"
api="http://$(docker port "$container" 8025/tcp | head -1)"
for _ in $(seq 50); do
  curl --silent --fail "$api/api/v1/info" >/dev/null 2>&1 && break
  sleep 0.2
done
curl --silent --fail "$api/api/v1/info" >/dev/null || fail "Mailpit did not start"

# run NAME APP_MODE [VAR=value...] -- args; sets $code.
run() {
  local name="$1" mode="$2"
  shift 2
  local settings=()
  while [[ "$1" != -- ]]; do settings+=("$1"); shift; done
  shift
  set +e
  env -i PATH="$PATH" APP_MODE="$mode" EMAIL_TRANSPORT=smtp SMTP_HOST=127.0.0.1 SMTP_PORT="$smtp_port" \
    SMTP_FROM=shop@example.com PUBLIC_SHOP_ORIGIN=http://127.0.0.1:8080 "${settings[@]}" \
    "$shopctl" email "$@" >"$temporary_dir/$name.out" 2>"$temporary_dir/$name.err"
  code=$?
  set -e
}

message_count() {
  curl --silent --fail "$api/api/v1/messages" | python3 -c 'import json,sys; print(json.load(sys.stdin)["total"])'
}

# 1. Development transport to a plaintext capture server delivers one message.
run send development SMTP_TLS=none -- test --to ops@example.com --timeout=10s
[[ "$code" == 0 ]] || fail "shopctl email test exited $code: $(cat "$temporary_dir/send.err")"
grep -q '"sent":true' "$temporary_dir/send.out" || fail "no success result"
[[ "$(message_count)" == 1 ]] || fail "Mailpit did not receive exactly one message"
id="$(curl --silent --fail "$api/api/v1/messages" | python3 -c 'import json,sys; print(json.load(sys.stdin)["messages"][0]["ID"])')"
curl --silent --fail "$api/api/v1/message/$id" >"$temporary_dir/message.json"
python3 - "$temporary_dir/message.json" <<'PYTHON' || fail "received message does not match"
import json, sys
message = json.load(open(sys.argv[1]))
assert message["From"]["Address"] == "shop@example.com", message["From"]
assert [r["Address"] for r in message["To"]] == ["ops@example.com"], message["To"]
assert message["Subject"] == "Тестовое письмо магазина «Слой»", message["Subject"]
assert "Это тестовое письмо магазина «Слой»." in message["Text"], message["Text"]
assert "http" not in message["Text"]
PYTHON

# 2. Production refuses plaintext SMTP before connecting.
run production-plaintext production SMTP_TLS=none SMTP_USERNAME=shop-user SMTP_PASSWORD="$password" -- test --to ops@example.com
[[ "$code" == 2 ]] || fail "production plaintext: exit $code"

# 3. Production STARTTLS against a server without STARTTLS fails; nothing is
#    sent in plaintext and the credentials are never offered.
run production-no-starttls production SMTP_TLS=starttls SMTP_USERNAME=shop-user SMTP_PASSWORD="$password" PUBLIC_SHOP_ORIGIN=https://shop.example.com -- test --to ops@example.com --timeout=10s
[[ "$code" == 1 ]] || fail "STARTTLS-less server: exit $code"
grep -q 'does not offer STARTTLS' "$temporary_dir/production-no-starttls.err" || fail "STARTTLS refusal not reported"
[[ "$(message_count)" == 1 ]] || fail "a message was delivered without TLS"

# 4. Usage errors.
run bad-recipient development SMTP_TLS=none -- test --to 'not an address'
[[ "$code" == 2 ]] || fail "invalid recipient: exit $code"
run unreachable development SMTP_TLS=none SMTP_PORT=1 -- test --to ops@example.com --timeout=5s
[[ "$code" == 1 ]] || fail "unreachable server: exit $code"

if grep -l -e "$password" -e shop-user "$temporary_dir"/*.out "$temporary_dir"/*.err; then
  fail "credentials printed"
fi
echo "email smtp check passed: one UTF-8 message delivered through Mailpit; plaintext production refused"

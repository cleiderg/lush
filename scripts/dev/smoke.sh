#!/usr/bin/env bash
# Builds and starts a local server, runs 50 bots for 20 seconds, and fails on
# any failed connection, lost connection or dropped frame.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

port="${SMOKE_PORT:-18080}"
tmp="$(mktemp -d)"
server_pid=""
cleanup() {
  [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null && wait "$server_pid" 2>/dev/null || true
  rm -rf "$tmp"
}
trap cleanup EXIT

(cd server && go build -o "$tmp/lush-server" ./cmd/server)
make --no-print-directory bots

LUSH_LISTEN_ADDR="127.0.0.1:$port" LUSH_SHUTDOWN_COUNTDOWN=0s "$tmp/lush-server" >"$tmp/server.log" 2>&1 &
server_pid=$!

for _ in $(seq 1 50); do
  curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1 && break
  kill -0 "$server_pid" 2>/dev/null || { cat "$tmp/server.log"; echo "smoke: server exited" >&2; exit 1; }
  sleep 0.2
done

if ! tools/bots/bin/bots -url "ws://127.0.0.1:$port/ws" -bots 50 -duration 20s -ramp 3s -radius 25 \
  -strict -out "$tmp/summary.json"; then
  cat "$tmp/summary.json" "$tmp/server.log" >&2 || true
  echo "smoke: FAILED" >&2
  exit 1
fi
cat "$tmp/summary.json"
echo "smoke: ok"

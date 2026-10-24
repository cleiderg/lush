#!/usr/bin/env bash
# Tests deploy/scripts/firewall.sh with a stubbed ufw.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../scripts/firewall.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cat >"$tmp/ufw" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$UFW_LOG"
STUB
chmod +x "$tmp/ufw"

fail() { echo "FAIL: $1" >&2; exit 1; }

run() { # run <log> [env assignments...]
  local log="$1"; shift
  : >"$log"
  env -u LUSH_SSH_SOURCES PATH="$tmp:$PATH" UFW_LOG="$log" "$@" bash "$script" 2>/dev/null
}

# Open to all (Q4).
log="$tmp/any.log"
run "$log" LUSH_SSH_SOURCES=any || fail "any: script failed"
expected="--force reset
default deny incoming
default allow outgoing
allow 22/tcp
allow 80/tcp
allow 443/tcp
--force enable"
[[ "$(cat "$log")" == "$expected" ]] || fail "any: unexpected rules: $(cat "$log")"
grep -q -i udp "$log" && fail "UDP rule present"

# Restricted sources.
log="$tmp/cidr.log"
run "$log" LUSH_SSH_SOURCES=203.0.113.4/32,2001:db8::/32 || fail "cidr: script failed"
expected="--force reset
default deny incoming
default allow outgoing
allow from 203.0.113.4/32 to any port 22 proto tcp
allow from 2001:db8::/32 to any port 22 proto tcp
allow 80/tcp
allow 443/tcp
--force enable"
[[ "$(cat "$log")" == "$expected" ]] || fail "cidr: unexpected rules: $(cat "$log")"

# Missing or invalid input exits non-zero before any ufw call.
for envs in "FOO=1" "LUSH_SSH_SOURCES=" "LUSH_SSH_SOURCES=bogus;rm"; do
  log="$tmp/bad.log"
  if run "$log" "$envs"; then fail "'$envs' should have failed"; fi
  [[ ! -s "$log" ]] || fail "'$envs' called ufw: $(cat "$log")"
done

echo "firewall_test: ok"

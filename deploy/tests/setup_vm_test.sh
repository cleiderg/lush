#!/usr/bin/env bash
# Tests deploy/scripts/setup-vm.sh with stubbed useradd, chown and systemctl.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../scripts/setup-vm.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir "$tmp/bin"
for cmd in useradd chown systemctl; do
  cat >"$tmp/bin/$cmd" <<'STUB'
#!/usr/bin/env bash
echo "$(basename "$0") $*" >>"$STUB_LOG"
STUB
  chmod +x "$tmp/bin/$cmd"
done

fail() { echo "FAIL: $1" >&2; exit 1; }
run() { PATH="$tmp/bin:$PATH" STUB_LOG="$tmp/stub.log" ROOT="$1" bash "$script" "${@:2}" >/dev/null 2>&1; }
mode() { stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"; }

# Missing deploy user: non-zero, nothing written, nothing stubbed called.
: >"$tmp/stub.log"
empty="$tmp/empty"; mkdir "$empty"
if run "$empty"; then fail "missing deploy user should fail"; fi
[[ -z "$(ls -A "$empty")" ]] || fail "missing deploy user wrote files"
[[ ! -s "$tmp/stub.log" ]] || fail "missing deploy user ran commands"

# First run.
r="$tmp/root"; mkdir "$r"
run "$r" deployer || fail "first run failed"
for e in production staging; do
  [[ -d "$r/opt/lush/$e/releases" ]] || fail "no releases dir for $e"
  grep -q "chown deployer $r/opt/lush/$e $r/opt/lush/$e/releases" "$tmp/stub.log" || fail "no chown for $e"
  cmp -s "$here/../systemd/lush-$e.service" "$r/etc/systemd/system/lush-$e.service" || fail "unit $e"
  [[ "$(mode "$r/etc/lush/$e.env")" == 600 ]] || fail "$e.env mode"
  cmp -s "$here/../env/$e.env.example" "$r/etc/lush/$e.env" || fail "$e.env content"
done
grep -q "useradd.* lush" "$tmp/stub.log" || fail "service user not created"
cmp -s "$here/../caddy/Caddyfile" "$r/etc/caddy/Caddyfile" || fail "Caddyfile"
grep -q "EnvironmentFile=/etc/lush/caddy.env" "$r/etc/systemd/system/caddy.service.d/lush.conf" || fail "caddy drop-in"
grep -q LUSH_PRODUCTION_HOST "$r/etc/lush/caddy.env" || fail "caddy.env"
grep -q LUSH_STAGING_HOST "$r/etc/lush/caddy.env" || fail "caddy.env staging"

sudoers="$r/etc/sudoers.d/lush-deploy"
expected="deployer ALL=(root) NOPASSWD: /bin/systemctl restart lush-production, /bin/systemctl restart lush-staging"
[[ "$(cat "$sudoers")" == "$expected" ]] || fail "sudoers content: $(cat "$sudoers")"
[[ "$(mode "$sudoers")" == 440 ]] || fail "sudoers mode"
if command -v visudo >/dev/null 2>&1; then
  visudo -cf "$sudoers" >/dev/null || fail "visudo rejects sudoers"
else
  echo "note: visudo not installed, skipped visudo -cf"
fi

# Second run keeps edits to env files.
echo "EDITED=1" >>"$r/etc/lush/production.env"
echo "LUSH_PRODUCTION_HOST=example.test" >"$r/etc/lush/caddy.env"
run "$r" deployer || fail "second run failed"
grep -q "EDITED=1" "$r/etc/lush/production.env" || fail "second run overwrote production.env"
grep -q example.test "$r/etc/lush/caddy.env" || fail "second run overwrote caddy.env"
[[ "$(cat "$sudoers")" == "$expected" ]] || fail "sudoers changed on rerun"

echo "setup_vm_test: ok"

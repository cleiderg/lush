#!/usr/bin/env bash
# One-time VM setup (docs/specs/operations.md). Run as root: setup-vm.sh <deploy-user>
#
# Installs the systemd units, the Caddyfile and a Caddy drop-in, env file
# stubs, the /opt/lush release layout and the narrow sudo rule that
# .github/workflows/deploy.yml needs. Safe to re-run: existing env files are
# never overwritten. Does not install packages or write secrets.
#
# ROOT (default /) prefixes every path, so tests can run into a temp dir.
set -euo pipefail

SERVICE_USER=lush
ROOT="${ROOT:-/}"
ROOT="${ROOT%/}"

if [[ $# -ne 1 || -z "$1" ]]; then
  echo "usage: setup-vm.sh <deploy-user>" >&2
  exit 1
fi
deploy_user="$1"
if [[ ! "$deploy_user" =~ ^[a-z_][a-z0-9_-]*$ ]]; then
  echo "setup-vm: invalid deploy user: '$deploy_user'" >&2
  exit 1
fi

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy_dir="$here/.."
envs=(production staging)

# Fail before writing anything if the sources are missing.
for f in caddy/Caddyfile systemd/lush-production.service systemd/lush-staging.service \
  env/production.env.example env/staging.env.example; do
  [[ -f "$deploy_dir/$f" ]] || { echo "setup-vm: missing $deploy_dir/$f" >&2; exit 1; }
done

# Service user (no login, no home).
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

# Release layout, writable by the deploy user.
for e in "${envs[@]}"; do
  mkdir -p "$ROOT/opt/lush/$e/releases"
  chown "$deploy_user" "$ROOT/opt/lush/$e" "$ROOT/opt/lush/$e/releases"
done
chmod 755 "$ROOT/opt/lush"

# systemd units.
mkdir -p "$ROOT/etc/systemd/system"
for e in "${envs[@]}"; do
  cp "$deploy_dir/systemd/lush-$e.service" "$ROOT/etc/systemd/system/lush-$e.service"
  chmod 644 "$ROOT/etc/systemd/system/lush-$e.service"
done

# Caddyfile and the drop-in that loads the hostnames.
mkdir -p "$ROOT/etc/caddy" "$ROOT/etc/systemd/system/caddy.service.d"
cp "$deploy_dir/caddy/Caddyfile" "$ROOT/etc/caddy/Caddyfile"
chmod 644 "$ROOT/etc/caddy/Caddyfile"
cat >"$ROOT/etc/systemd/system/caddy.service.d/lush.conf" <<'DROPIN'
[Service]
EnvironmentFile=/etc/lush/caddy.env
DROPIN
chmod 644 "$ROOT/etc/systemd/system/caddy.service.d/lush.conf"

# Env files: created once, never overwritten (the owner edits them in place).
mkdir -p "$ROOT/etc/lush"
chmod 755 "$ROOT/etc/lush"
for e in "${envs[@]}"; do
  if [[ ! -e "$ROOT/etc/lush/$e.env" ]]; then
    cp "$deploy_dir/env/$e.env.example" "$ROOT/etc/lush/$e.env"
  fi
  chmod 600 "$ROOT/etc/lush/$e.env"
done
if [[ ! -e "$ROOT/etc/lush/caddy.env" ]]; then
  cat >"$ROOT/etc/lush/caddy.env" <<'CADDYENV'
# Hostnames served by Caddy. Fill in on the server; do not commit.
LUSH_PRODUCTION_HOST=
LUSH_STAGING_HOST=
CADDYENV
fi
chmod 600 "$ROOT/etc/lush/caddy.env"

# Sudo rule: exactly two restarts, no password, nothing else. Validate a temp
# copy first so a bad file never lands in sudoers.d.
mkdir -p "$ROOT/etc/sudoers.d"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
cat >"$tmp" <<SUDOERS
$deploy_user ALL=(root) NOPASSWD: /bin/systemctl restart lush-production, /bin/systemctl restart lush-staging
SUDOERS
if command -v visudo >/dev/null 2>&1; then
  visudo -cf "$tmp" >/dev/null
else
  echo "setup-vm: visudo not found, sudoers file not validated" >&2
fi
rm -f "$ROOT/etc/sudoers.d/lush-deploy"
cp "$tmp" "$ROOT/etc/sudoers.d/lush-deploy"
chmod 440 "$ROOT/etc/sudoers.d/lush-deploy"
if command -v visudo >/dev/null 2>&1; then
  visudo -cf "$ROOT/etc/sudoers.d/lush-deploy" >/dev/null
fi

# Pick up the new units; enable them but do not start (no release exists yet).
systemctl daemon-reload
for e in "${envs[@]}"; do
  systemctl enable "lush-$e"
done

echo "setup-vm: done. Edit $ROOT/etc/lush/*.env, then deploy."

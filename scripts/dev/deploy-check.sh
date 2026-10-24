#!/usr/bin/env bash
# Checks deploy/ config: systemd units, env templates. Used by `make deploy-check`.
# Usage: deploy-check.sh [deploy-dir]     (default: deploy/ next to scripts/)
#        deploy-check.sh --self-test      (proves the checks reject bad input)
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"

if [[ "${1:-}" == "--self-test" ]]; then
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  fail=0
  expect_fail() { # name, sed expression, file
    rm -rf "$tmp/d"; cp -R "$root/deploy" "$tmp/d"
    sed -i.bak -e "$2" "$tmp/d/$3"; rm -f "$tmp/d/$3.bak"
    if DEPLOY_CHECK_NO_SYSTEMD=1 "$0" "$tmp/d" >/dev/null 2>&1; then
      echo "self-test: $1 was NOT rejected"; fail=1
    else echo "self-test: $1 rejected (ok)"; fi
  }
  expect_fail "password in database URL" 's#^LUSH_DATABASE_URL=.*#LUSH_DATABASE_URL=postgres://lush:hunter2@DB_HOST:5432/lush_production?sslmode=require#' env/production.env.example
  expect_fail "non-placeholder database URL" 's#^LUSH_DATABASE_URL=.*#LUSH_DATABASE_URL=postgres://lush@db.internal:5432/lush_production?sslmode=require#' env/production.env.example
  expect_fail "database URL without TLS" 's#?sslmode=require##' env/staging.env.example
  expect_fail "short TimeoutStopSec" 's#^TimeoutStopSec=.*#TimeoutStopSec=30s#' systemd/lush-staging.service
  expect_fail "unknown LUSH_ variable" 's#^LUSH_ENV=#LUSH_BOGUS=#' env/staging.env.example
  expect_fail "shared port" 's#127.0.0.1:8081#127.0.0.1:8080#' env/staging.env.example
  expect_fail "shared database" 's#lush_staging#lush_production#' env/staging.env.example
  expect_fail "Caddy port differs from env" 's#reverse_proxy 127.0.0.1:8081#reverse_proxy 127.0.0.1:9999#' caddy/Caddyfile
  # shellcheck disable=SC2016 # literal Caddy placeholder, not a shell expansion
  expect_fail "hardcoded Caddy hostname" 's#{$LUSH_STAGING_HOST}#staging.example.com#' caddy/Caddyfile
  expect_fail "HTTP/3 enabled" 's#protocols h1 h2#protocols h1 h2 h3#' caddy/Caddyfile
  expect_fail "root user" 's#^User=lush#User=root#' systemd/lush-production.service
  exit $fail
fi

dir="${1:-$root/deploy}"
config="$root/server/internal/ops/config.go"
errs=0
bad() { echo "deploy-check: $*" >&2; errs=1; }

# Seconds from a systemd time span like 150s, 2min, 1min 30s.
to_secs() {
  local v="$1" total=0 tok n
  for tok in $v; do
    n="${tok%%[a-z]*}"
    case "${tok#"$n"}" in
      ""|s|sec) total=$((total + n)) ;;
      min|m) total=$((total + n * 60)) ;;
      *) echo 0; return ;;
    esac
  done
  echo "$total"
}

# Names the server reads: every "LUSH_*" string literal in config.go.
known="$(grep -o '"LUSH_[A-Z_]*"' "$config" | tr -d '"' | sort -u)"
[[ -n "$known" ]] || bad "no LUSH_ names found in $config"

declare -a ports dbs
for e in production staging; do
  unit="$dir/systemd/lush-$e.service"
  envf="$dir/env/$e.env.example"
  [[ -f "$unit" ]] || { bad "missing $unit"; continue; }
  [[ -f "$envf" ]] || { bad "missing $envf"; continue; }

  grep -qx "ExecStart=/opt/lush/$e/current/lush-server" "$unit" || bad "$e: ExecStart must be /opt/lush/$e/current/lush-server"
  grep -qx "Restart=on-failure" "$unit" || bad "$e: Restart=on-failure required"
  grep -qx "EnvironmentFile=/etc/lush/$e.env" "$unit" || bad "$e: EnvironmentFile=/etc/lush/$e.env required"
  user="$(sed -n 's/^User=//p' "$unit")"
  if [[ -z "$user" || "$user" == root || "$user" == 0 ]]; then bad "$e: User= must be a non-root user"; fi
  t="$(sed -n 's/^TimeoutStopSec=//p' "$unit")"
  if [[ -z "$t" || "$(to_secs "$t")" -lt 90 ]]; then bad "$e: TimeoutStopSec must be >= 90s (got '${t:-unset}')"; fi

  declare -A kv=()
  while IFS= read -r line; do
    [[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
    if [[ "$line" =~ ^(LUSH_[A-Z_]*)=(.*)$ ]]; then
      kv[${BASH_REMATCH[1]}]="${BASH_REMATCH[2]}"
      grep -qx "${BASH_REMATCH[1]}" <<<"$known" || bad "$e: ${BASH_REMATCH[1]} is not read by config.go"
    else bad "$e: unexpected line in env example: $line"; fi
  done <"$envf"

  [[ "${kv[LUSH_ENV]:-}" == "$e" ]] || bad "$e: LUSH_ENV must be $e"
  addr="${kv[LUSH_LISTEN_ADDR]:-}"
  [[ "$addr" =~ ^127\.0\.0\.1:([0-9]+)$ ]] || bad "$e: LUSH_LISTEN_ADDR must be 127.0.0.1:<port>"
  ports+=("${BASH_REMATCH[1]:-}")
  [[ -n "${kv[LUSH_WS_ORIGINS]:-}" ]] || bad "$e: LUSH_WS_ORIGINS required"
  url="${kv[LUSH_DATABASE_URL]:-}"
  # Only a placeholder is allowed: a user with no password, the literal host
  # DB_HOST, and TLS to the managed cluster (D-007).
  if [[ "$url" =~ ^postgres(ql)?://[a-z_]+@DB_HOST:[0-9]+/([a-z_]+)\?sslmode=require$ ]]; then
    dbs+=("${BASH_REMATCH[2]}")
  else
    bad "$e: LUSH_DATABASE_URL must be the placeholder postgres://<user>@DB_HOST:<port>/<db>?sslmode=require (no password, no real host)"
    dbs+=("")
  fi
  unset kv
done

if [[ "${#ports[@]}" -eq 2 ]]; then
  [[ "${ports[0]}" != "${ports[1]}" ]] || bad "production and staging share port ${ports[0]}"
  [[ "${dbs[0]}" != "${dbs[1]}" || -z "${dbs[0]}" ]] || bad "production and staging share database ${dbs[0]}"
fi

# Caddyfile: two site blocks on the env-example ports, HTTP/3 off.
caddyfile="$dir/caddy/Caddyfile"
if [[ ! -f "$caddyfile" ]]; then
  bad "missing $caddyfile"
else
  for pair in "production:${ports[0]:-}" "staging:${ports[1]:-}"; do
    e="${pair%%:*}"; port="${pair#*:}"
    host="LUSH_$(tr '[:lower:]' '[:upper:]' <<<"$e")_HOST"
    # Body of the site block opened by {$LUSH_<ENV>_HOST} {
    block="$(awk -v h="{\$$host} {" '$0 == h {f=1; next} f && /^}/ {exit} f' "$caddyfile")"
    if [[ -z "$block" ]]; then bad "caddy: no site block for {\$$host}"; continue; fi
    ups="$(sed -n 's/^[[:space:]]*reverse_proxy[[:space:]]\{1,\}//p' <<<"$block")"
    [[ "$ups" == "127.0.0.1:$port" ]] || bad "caddy: $e must reverse_proxy to 127.0.0.1:$port (got '${ups:-none}')"
  done
  # shellcheck disable=SC2016 # literal Caddy placeholder, not a shell expansion
  [[ "$(grep -c '^{\$LUSH_[A-Z]*_HOST} {' "$caddyfile")" -eq 2 ]] || bad "caddy: expected exactly two {\$LUSH_*_HOST} site blocks"
  grep -qE '^[[:space:]]*protocols h1 h2$' "$caddyfile" || bad "caddy: global servers { protocols h1 h2 } required"
  if command -v caddy >/dev/null 2>&1; then
    export LUSH_PRODUCTION_HOST=production.example.test LUSH_STAGING_HOST=staging.example.test
    if ! adapted="$(caddy adapt --adapter caddyfile --config "$caddyfile" 2>&1)"; then
      echo "$adapted" >&2; bad "caddy adapt failed"
    elif grep -q '"h3"' <<<"$adapted"; then
      bad "caddy: adapted config enables h3"
    fi
    caddy validate --adapter caddyfile --config "$caddyfile" >/dev/null 2>&1 || { caddy validate --adapter caddyfile --config "$caddyfile" >&2 || true; bad "caddy validate failed"; }
  elif [[ "${CI:-}" == "true" ]]; then
    bad "caddy not installed (required when CI=true)"
  else
    echo "deploy-check: caddy not found, skipping validate (set CI=true to require)"
  fi
fi

# systemd-analyze verify
if [[ -z "${DEPLOY_CHECK_NO_SYSTEMD:-}" ]]; then
  if command -v systemd-analyze >/dev/null 2>&1; then
    # Verify resolves ExecStart paths on this machine; they don't exist here, so
    # ignore only that complaint.
    out="$(systemd-analyze verify "$dir"/systemd/lush-production.service "$dir"/systemd/lush-staging.service 2>&1 || true)"
    out="$(grep -v -e 'is not executable' -e 'No such file or directory' -e 'Failed to prepare filesystem' <<<"$out" || true)"
    if [[ -n "$out" ]]; then echo "$out" >&2; bad "systemd-analyze verify reported problems"; fi
  elif [[ "${CI:-}" == "true" ]]; then
    bad "systemd-analyze not installed (required when CI=true)"
  else
    echo "deploy-check: systemd-analyze not found, skipping unit verify (set CI=true to require)"
  fi
fi

[[ $errs -eq 0 ]] && echo "deploy-check: ok"
exit $errs

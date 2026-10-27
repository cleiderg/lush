#!/usr/bin/env bash
# Tests deploy/scripts/sentry-sourcemaps.sh with a stubbed SENTRY_CLI.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../scripts/sentry-sourcemaps.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

token="sntrys_test_token_d0n0tl34k"

# The stub logs its arguments and the env it saw, and echoes its arguments to
# stdout; STUB_FAIL=1 makes it fail.
cat >"$tmp/sentry-cli" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$STUB_LOG"
echo "org=$SENTRY_ORG project=$SENTRY_PROJECT token_set=${SENTRY_AUTH_TOKEN:+yes}" >>"$STUB_LOG"
echo "stub: $*"
[[ "${STUB_FAIL:-}" != "1" ]]
STUB
chmod +x "$tmp/sentry-cli"

fail() { echo "FAIL: $1" >&2; exit 1; }

mkdist() { # mkdist <dir> [maps]
  rm -rf "$1"; mkdir -p "$1/assets"
  echo "console.log(1)" >"$1/assets/index-abc.js"
  echo "<html></html>" >"$1/index.html"
  if [[ "${2:-}" == "maps" ]]; then
    echo '{"version":3}' >"$1/assets/index-abc.js.map"
    echo '{"version":3}' >"$1/assets/vendor.js.map"
  fi
}

out="$tmp/out"
run() { # run <dir> [env assignments...]; stdout and stderr go to $out
  local dir="$1"; shift
  : >"$tmp/stub.log"
  env -u SENTRY_AUTH_TOKEN -u SENTRY_ORG -u SENTRY_PROJECT -u STUB_FAIL \
    SENTRY_CLI="$tmp/sentry-cli" STUB_LOG="$tmp/stub.log" "$@" \
    bash "$script" "$dir" deadbeef1234 >"$out" 2>&1
}

no_maps_left() { [[ -z "$(find "$1" -name '*.map')" ]] || fail "$2: .map files left: $(find "$1" -name '*.map')"; }
js_kept() { [[ -f "$1/assets/index-abc.js" && -f "$1/index.html" ]] || fail "$2: .js or .html deleted"; }
no_token() { ! grep -q "$token" "$out" || fail "$1: token appeared in output"; }
stub_not_called() { [[ ! -s "$tmp/stub.log" ]] || fail "$1: stub was called: $(cat "$tmp/stub.log")"; }

all=(SENTRY_AUTH_TOKEN="$token" SENTRY_ORG=lush SENTRY_PROJECT=lush-client)

# AC2: all set, the stub gets the release and directory, maps go, js stays.
d="$tmp/dist"; mkdist "$d" maps
run "$d" "${all[@]}" || fail "upload: script failed: $(cat "$out")"
[[ "$(head -1 "$tmp/stub.log")" == "sourcemaps upload --release deadbeef1234 $d" ]] \
  || fail "upload: unexpected stub args: $(cat "$tmp/stub.log")"
[[ "$(sed -n 2p "$tmp/stub.log")" == "org=lush project=lush-client token_set=yes" ]] \
  || fail "upload: stub did not get the Sentry env: $(sed -n 2p "$tmp/stub.log")"
no_maps_left "$d" upload; js_kept "$d" upload; no_token upload

# AC3: no token, upload skipped with a message, maps still deleted, exit 0.
mkdist "$d" maps
run "$d" SENTRY_ORG=lush SENTRY_PROJECT=lush-client || fail "no token: script failed: $(cat "$out")"
grep -q "skipping upload" "$out" || fail "no token: no skip message: $(cat "$out")"
stub_not_called "no token"; no_maps_left "$d" "no token"; js_kept "$d" "no token"

# AC3: token set but org or project missing, non-zero without calling the stub.
mkdist "$d" maps
if run "$d" SENTRY_AUTH_TOKEN="$token" SENTRY_PROJECT=lush-client; then fail "no SENTRY_ORG: should have failed"; fi
stub_not_called "no SENTRY_ORG"; no_token "no SENTRY_ORG"
mkdist "$d" maps
if run "$d" SENTRY_AUTH_TOKEN="$token" SENTRY_ORG=lush; then fail "no SENTRY_PROJECT: should have failed"; fi
stub_not_called "no SENTRY_PROJECT"; no_token "no SENTRY_PROJECT"

# AC3: no .map files, message and exit 0 without calling the stub.
mkdist "$d"
run "$d" "${all[@]}" || fail "no maps: script failed: $(cat "$out")"
grep -q "no .map files" "$out" || fail "no maps: no message: $(cat "$out")"
stub_not_called "no maps"; js_kept "$d" "no maps"

# AC3: the stub fails, the script exits non-zero (maps are still removed).
mkdist "$d" maps
if run "$d" "${all[@]}" STUB_FAIL=1; then fail "stub failure: should have failed"; fi
[[ -s "$tmp/stub.log" ]] || fail "stub failure: stub not called"
no_maps_left "$d" "stub failure"; no_token "stub failure"

# Missing arguments or directory exit non-zero.
if SENTRY_CLI="$tmp/sentry-cli" bash "$script" >/dev/null 2>&1; then fail "no args: should have failed"; fi
if SENTRY_CLI="$tmp/sentry-cli" bash "$script" "$tmp/nope" abc >/dev/null 2>&1; then fail "missing dir: should have failed"; fi

# The default sentry-cli is pinned to an exact version.
grep -Eq '^SENTRY_CLI_VERSION="[0-9]+\.[0-9]+\.[0-9]+"$' "$script" || fail "sentry-cli version not pinned"

echo "sentry_sourcemaps_test: ok"

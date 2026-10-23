#!/usr/bin/env bash
# Print versions of the tools needed to build Lush; exit 1 naming any missing.
missing=()
check() {
  local name=$1; shift
  if command -v "$name" >/dev/null 2>&1; then
    printf '%s: %s\n' "$name" "$("$name" "$@" 2>&1 | head -n1)"
  else
    missing+=("$name")
  fi
}
check go version
check node --version
check flatc --version
if [ ${#missing[@]} -gt 0 ]; then
  echo "missing: ${missing[*]}" >&2
  exit 1
fi

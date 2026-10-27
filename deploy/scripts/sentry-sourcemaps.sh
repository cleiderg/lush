#!/usr/bin/env bash
# Upload the built client's source maps to Sentry for a release, then delete
# the .map files so Cloudflare Pages never serves them (PRD 6, D-008).
#
# Usage: sentry-sourcemaps.sh <dist-dir> <release-sha>
# Environment: SENTRY_AUTH_TOKEN, SENTRY_ORG, SENTRY_PROJECT (read by
# sentry-cli itself, never printed). Without SENTRY_AUTH_TOKEN the upload is
# skipped and the maps are still deleted. SENTRY_CLI overrides the pinned
# sentry-cli command.
set -euo pipefail

SENTRY_CLI_VERSION="3.8.2"
SENTRY_CLI="${SENTRY_CLI:-npx --yes @sentry/cli@${SENTRY_CLI_VERSION}}"

dir="${1:-}"
release="${2:-}"
if [[ -z "$dir" || -z "$release" ]]; then
  echo "sentry-sourcemaps: usage: $0 <dist-dir> <release-sha>" >&2
  exit 1
fi
if [[ ! -d "$dir" ]]; then
  echo "sentry-sourcemaps: directory not found: $dir" >&2
  exit 1
fi

maps="$(find "$dir" -type f -name '*.map' | wc -l | tr -d ' ')"
if [[ "$maps" -eq 0 ]]; then
  echo "sentry-sourcemaps: no .map files in $dir, nothing to upload"
  exit 0
fi

status=0
if [[ -z "${SENTRY_AUTH_TOKEN:-}" ]]; then
  echo "sentry-sourcemaps: SENTRY_AUTH_TOKEN not set, skipping upload"
elif [[ -z "${SENTRY_ORG:-}" || -z "${SENTRY_PROJECT:-}" ]]; then
  echo "sentry-sourcemaps: SENTRY_ORG and SENTRY_PROJECT are required with SENTRY_AUTH_TOKEN" >&2
  status=1
else
  echo "sentry-sourcemaps: uploading $maps source maps from $dir for release $release"
  read -r -a cli <<<"$SENTRY_CLI"
  if ! "${cli[@]}" sourcemaps upload --release "$release" "$dir"; then
    echo "sentry-sourcemaps: upload failed" >&2
    status=1
  fi
fi

# Delete the maps even when the upload failed: they must never be served.
find "$dir" -type f -name '*.map' -delete
echo "sentry-sourcemaps: deleted $maps .map files from $dir"
exit "$status"

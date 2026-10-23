#!/usr/bin/env bash
# Configure the VM firewall (docs/specs/operations.md, Firewall).
#
# Required: LUSH_SSH_SOURCES, comma-separated list of sources allowed to reach
# SSH (22). Use "any" for open to all (the Phase 1 decision: key-only SSH, since
# GitHub Actions deploy IPs change), or CIDRs such as "203.0.113.4/32".
# There is deliberately no UDP 443 rule until WebTransport ships.
set -euo pipefail

if [[ -z "${LUSH_SSH_SOURCES:-}" ]]; then
  echo "firewall: LUSH_SSH_SOURCES is required (\"any\" or comma-separated CIDRs)" >&2
  exit 1
fi

IFS=',' read -r -a sources <<<"$LUSH_SSH_SOURCES"
for src in "${sources[@]}"; do
  if [[ "$src" != "any" && ! "$src" =~ ^[0-9a-fA-F.:]+(/[0-9]+)?$ ]]; then
    echo "firewall: invalid SSH source: '$src'" >&2
    exit 1
  fi
done

ufw default deny incoming
ufw default allow outgoing
for src in "${sources[@]}"; do
  if [[ "$src" == "any" ]]; then
    ufw allow 22/tcp
  else
    ufw allow from "$src" to any port 22 proto tcp
  fi
done
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

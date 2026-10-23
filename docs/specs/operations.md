# Operations

**Part of:** [[PRD]] · **Status:** Draft

> Deploying and running Lush. Owner: Infra.

- Domain with DNS on Cloudflare.
- GitHub Actions: client → Cloudflare Pages; Go server → Vultr VM.
- systemd service with automatic restart; Caddy for HTTPS.
- Graceful restarts: in-game countdown, save everyone, restart.
- Sentry on client and server.

## Environments

- **Production:** Vultr CPU Optimized VM, 2 vCPU / 4 GB, Los Angeles; Vultr Managed PostgreSQL, Los Angeles, same private network.
- **Staging:** a second instance of the server on the **same VM**, on a separate port, with its own database (or schema) so test data never mixes with production (D-032).
  - Run the CI load test only when no testers are online.
  - Production tick metrics recorded during a load test are not reliable.
  - Move staging to its own VM before any public launch.

## Firewall

- SSH (22): open to all, key-only (password login off). Phase 1 has internal testers only and deploys come from GitHub Actions, whose IPs change. Revisit with a tunnel before public launch.
- TCP 80 and 443: open.
- UDP 443: open only once WebTransport ships.

## Database access

- Trusted sources: the game server, plus the owner's IP during development.
- Automatic backups on.

See also [[Monitoring]].

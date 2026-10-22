# Persistence

**Part of:** [[PRD]] · **Status:** Draft

> How player and world data is stored. Owner: Server platform.

- `pgx` with connection pooling; migrations with goose or golang-migrate, versioned in git.
- Trades and purchases: saved immediately in a transaction.
- Position and frequently changing state: saved every few seconds and on logout.
- Player records carry a version number; stale saves are rejected.
- Economy audit log: every trade and item creation is recorded.

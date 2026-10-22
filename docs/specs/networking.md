# Networking

**Part of:** [[PRD]] · **Status:** Draft

> How the client and server talk. Owners: Protocol, Server platform, Client.

- Binary protocol defined once in `protocol/schema` using **FlatBuffers**, generated for Go and TypeScript with `flatc`. Generated code is checked by CI so it can't drift from the schema.
- Interest management: each client receives only players within view radius, capped at the nearest N (100 in testing).
- Client prediction with server reconciliation; every input is numbered.
- Interpolation of other players.
- Protocol version sent at connect; mismatched clients are told to reload.
- Reconnect without losing progress (wifi ↔ cellular).
- Later: delta snapshots against acknowledged snapshots; priority accumulator for crowded areas.

## Transport

- Phase 1: WebSocket over HTTPS (Caddy in front).
- Later: WebTransport (HTTP/3) with automatic WebSocket fallback. Snapshots as datagrams (keep each under ~1,200 bytes), chat and trades on reliable streams.
- Log which transport each player uses.

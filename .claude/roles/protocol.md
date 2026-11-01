---
name: protocol
description: Owns the FlatBuffers schema and the generated Go and TypeScript protocol code. Issues labeled area:protocol.
model: sonnet
maxTurns: 80
---

# Role: protocol agent

You own `protocol/`, `server/internal/protocol/` (generated) and `client/src/protocol/` (generated). Read `docs/specs/networking.md` and PRD sections 7 and 12.

- The schema in `protocol/schema/` is the single source of truth. Generated code is produced only by `make protocol` and committed with the schema change.
- Every schema change bumps the protocol version constant. Clients with a different version are told to reload.
- Keep messages small. A snapshot with 100 entities must stay well under 1,200 bytes so it can later travel as one WebTransport datagram.
- Prefer additive changes (new optional fields) so older generated code keeps working.
- The generated code needs the FlatBuffers runtime. You may edit `server/go.mod`, `server/go.sum`, `client/package.json` and `client/package-lock.json`, but only to add or bump that runtime (`github.com/google/flatbuffers` and the `flatbuffers` npm package). Change nothing else in those files.
- `protocol/gen/generate.sh` must start every generated TypeScript file with `/* eslint-disable */` and `// @ts-nocheck`, so generated code passes the client's lint and type check without changes to client config.

Before finishing, run `make protocol`, then confirm the Go and TypeScript code both compile and their tests pass (`cd server && go build ./... && go test ./...`, and `cd client && npm ci && npx tsc --noEmit && npm test -- --run` once those projects exist).

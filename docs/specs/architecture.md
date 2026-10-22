# Architecture

**Part of:** [[PRD]] · **Status:** Draft

> Module layout and rules for the Lush client and server. Owners: Server world, Server platform, Client (changes to this spec: the owner).

## Modular monolith

One server binary and one client app, each split into modules with strict dependency rules. Game logic never depends on the network, the database, or Three.js; those plug in at the edges through interfaces.

**Repo layout (monorepo):**

```
lush/
├─ protocol/            # single source of truth for messages
│  ├─ schema/
│  └─ gen/              # generator → Go + TS code
├─ shared/map/          # campus data + collision grid built from OpenStreetMap
├─ server/
│  ├─ cmd/server/       # main.go: wiring only
│  └─ internal/
│     ├─ world/         # simulation, tick loop, entities. No I/O.
│     ├─ systems/       # feature modules: movement/, chat/, combat/, trade/
│     ├─ interest/      # spatial grid, who-sees-whom
│     ├─ transport/     # websocket/ (webtransport/ later), one interface
│     ├─ session/       # accounts, tokens, reconnect
│     ├─ store/         # Postgres via pgx, behind interfaces
│     ├─ api/           # HTTP: inventory, shop, trades, profiles (housing in a later phase)
│     ├─ protocol/      # generated, never hand-edited
│     └─ ops/           # config, metrics, logging, graceful shutdown
├─ client/src/
│  ├─ core/             # game loop, clock, event bus
│  ├─ net/              # transport interface + implementations
│  ├─ protocol/         # generated
│  ├─ state/            # world state, prediction, interpolation. No Three.js.
│  ├─ render/           # all Three.js code
│  ├─ input/            # joystick, buttons, keyboard
│  ├─ ui/               # HTML/CSS: chat, menus, inventory
│  └─ audio/
├─ tools/bots/          # load tester, uses the same protocol code
└─ db/migrations/
```

**Dependency rules:**
- Server: `transport`, `store`, `api` → `world`/`systems`. `world` never imports `transport` or `store`.
- Client: `render`, `ui`, `audio` → `state` → `protocol`. `state` never imports Three.js (enforced with an ESLint boundaries rule).
- Generated `protocol` code is never edited by hand.
- `cmd/server/main.go` is the only place modules are wired together.

**Feature modules** implement one interface and are registered in `main.go`:

```go
type System interface {
    Name() string
    Handle(msg protocol.Message, p *world.Player)
    Tick(w *world.World, dt time.Duration)
}
```

## Simulation and threading

- One fixed world tick drives all game logic. Combat and skills run every Nth tick (0.6 s).
- Only the world loop mutates world state. Transport goroutines push incoming messages onto a queue; the tick applies them.
- Database writes happen off the tick, except trades, which complete only after the transaction commits.

- **Movement tick: 20 Hz** (50 ms budget). Combat runs every 12th tick (0.6 s). The tick rate is a config value so it can be changed after playtests.

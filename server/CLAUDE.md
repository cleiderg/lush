# Lush server (Go)

One binary: `server/cmd/server`. Specs: `docs/specs/architecture.md`, `networking.md`, `persistence.md`, `gameplay.md`.

## Commands (run from `server/`)

- `go build ./...`
- `go vet ./...`
- `go test ./...` (add `-race` for anything touching goroutines)
- `gofmt -l .` must print nothing

## Conventions

- Modules live in `internal/`. `cmd/server/main.go` only wires modules together.
- **Only the world tick mutates world state.** Transport goroutines push commands onto a queue; the tick applies them.
- Movement tick is 20 Hz (config value). Combat runs every 12th tick (0.6 s).
- Feature modules implement `world.System` (`Name`, `Handle`, `Tick`) and are registered in `main.go`.
- Database writes happen off the tick, except trades and PvP drops, which complete only after their transaction commits.
- Inject time through a clock interface so tests are deterministic. Never call `time.Now()` inside `world/` or `systems/`.
- Prefer table-driven tests. Benchmarks for anything that runs per tick per player.
- Postgres via `pgx` with `pgxpool`. Migrations in `db/migrations/`.
- Errors: wrap with context (`fmt.Errorf("load player %d: %w", id, err)`). No panics outside `main`.

# Lush bots

Load tester used by `make smoke` and `make loadtest`.

The bots use the generated protocol code. `internal/protocol` is a build-time
copy of `server/internal/protocol` (Go forbids importing another module's
`internal` package); `make bots` refreshes it and it is not committed.

```
make smoke
make loadtest LOADTEST_URL=wss://host/ws LOADTEST_OUT=result.json
```

Each bot sends Hello, then walks inside a circle (default radius 25 m around
its spawn) at 10 Hz, chats every 20-40 s (the first within 20 s) and pings
every 2 s. The JSON summary has `connected_at_end`, `failed_to_connect`,
`download_kb_per_s_per_player`, `rtt_ms_p50`, `rtt_ms_p99` and `server` (the
`/metrics` document taken at the end of the steady period).
`server_max_dropped_per_s` is the highest `dropped_per_s` seen while sampling
`/metrics` twice a second for the whole run, ramp included. Bots steer by their
own position from each Snapshot.

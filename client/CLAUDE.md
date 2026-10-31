# Lush client (TypeScript + Three.js + Vite)

Specs: `docs/specs/architecture.md`, `networking.md`, `map.md`. Controls and style: PRD section 5.

## Commands (run from `client/`)

- `npm ci`
- `npm run dev`: local dev server
- `npm run lint`: ESLint, including the import-boundary rule
- `npm test`: unit tests (Vitest)
- `npm run build`: production bundle in `dist/`

## Conventions

- Three.js is pinned to an exact version in `package.json`. Upgrades are their own issue.
- `src/state` holds game state, prediction, and interpolation, and never imports `three` or `src/render`. `src/render` only reads state.
- `src/net` exposes one transport interface (`send(bytes)`, `onMessage(bytes)`). WebSocket now; WebTransport later behind the same interface.
- Number every input for server reconciliation.
- Frame rate is capped at 30 fps. Device floor: iPhone 13 and comparable Android, current mobile Safari and Chrome.
- Low-poly, mostly flat colors; small tiling textures only for floors, walls and paths (D-050). Chunky, big-headed characters. Match the look in docs/specs/visual-style.md. Use instancing for anything repeated (other players, trees, props).
- UI is plain HTML/CSS in `src/ui`. Portrait mobile layout: joystick bottom-left, action buttons bottom-right, chat mode replaces the controls area with the in-game keyboard.
- The game server address comes from `import.meta.env.VITE_SERVER_URL` (set per environment by the deploy workflow); never hard-code it. In development, default to `ws://localhost:8080/ws`.
- Start audio only after the first user tap.
- Show "© OpenStreetMap contributors" in the credits.

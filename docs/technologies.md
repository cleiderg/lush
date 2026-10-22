Client: Typescript, Three,js HTML CSS
Game Server: Go, WebSocket
Client Host: CloudFlare Pages
Server Host: Vultr VM 2 dedicated CPU's, 4gb of memory with Caddy for HTTPS
Database: Vultr managed PostgreSQL in Los Angeles

**Client tooling**

- **Vite** for building the client, with Three.js installed through npm and pinned to a version.
- **glTF models from Blender**, compressed with meshopt or Draco, for anything not built in code.
- **A home-screen web app setup (PWA manifest).** Lets students “Add to Home Screen” and play full-screen without Safari’s address bar. That matters a lot for a mobile browser game.
- **Audio** through the Web Audio API or Howler.js. Phones only allow sound after a tap, so start audio on the first touch.

**Networking details**

- **A shared protocol definition** that both the Go server and the TypeScript client follow, so the binary message formats can’t drift apart. A small schema file that generates code for both is safest.
- **Client prediction and interpolation:** your own character moves instantly, and other players are smoothed between updates.
- **Tick rates:** 10–20 Hz for movement and 0.6 s for combat, written down as decisions.
- **Reconnecting:** when a phone switches from wifi to cellular, the player should drop back in without losing progress.
- **WebTransport** as a later upgrade, with WebSocket kept as the fallback.

**Server and database code**

- **`pgx`,** the standard Go Postgres driver, with connection pooling.
- **A migration tool** (goose or golang-migrate) so database changes are versioned in git.
- **A save strategy:**
    - Trades and purchases are saved immediately, inside a database transaction, so items can’t be duplicated.
    - Position and other frequently changing state is saved every few seconds and on logout.
- **A regular HTTP API in the same Go program** for anything that isn’t real-time: inventory, shop, housing, profiles.
- **The server decides everything that matters.** Never trust the phone for combat, loot, or money.

**Map data**

- **OpenStreetMap requires credit.** Its license requires visible attribution, like “© OpenStreetMap contributors” somewhere in the game or the credits.
- **Collision data the server also uses,** so walls work the same on the phone and on the server.

**Deploying and running it**

- **A domain name,** with DNS on Cloudflare.
- **GitHub Actions** that build the client and deploy it to Cloudflare Pages, and build the Go server and deploy it to the VM.
- **A systemd service** so the server restarts itself after a crash or reboot.
- **Secrets** (database password, email API key) kept in environment files on the server, never in git.
- **Deploying without kicking everyone:** at minimum, a “server restarting in 2 minutes” message and saving everyone first.

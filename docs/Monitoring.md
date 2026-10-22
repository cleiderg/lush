Use Sentry for catching errors, and if you want, have Lantern work on top of it. They do different jobs, and they combine well.

**What Sentry does that Lantern doesn’t:**

- **Captures errors where they happen.** Its SDK runs inside players’ browsers and inside your Go server, and reports crashes the moment they occur, including on phones you’ll never see.
- **Turns minified code back into real lines.** Your Vite build squashes the TypeScript into unreadable code. Sentry uses source maps to show the actual file and line in your source.
- **Groups duplicates.** A thousand players hitting the same bug shows up as one issue with a count.
- **Adds context:** browser, device, OS version, which release, and what the player did just before.
- **Runs 24/7 in the cloud.** Lantern runs locally on your machine, so errors from players at 2 a.m. while your laptop is closed would be lost.

To get all that from Lantern, you’d have to build a browser error reporter, an endpoint to receive the reports, source-map handling, grouping, and hosting. That’s months of work that isn’t your game. You also don’t want the game’s reliability depending on an experimental tool you’re still building.

**Where Lantern fits.** Its design already has collectors like `inbox.bugs`, `logs.scan`, and `ci.watch`. A Sentry collector would slot right in:

- Sentry catches and groups the error.
- Lantern pulls new issues from Sentry’s API, files them as tickets, and starts triaging. For example, it can find the code involved, check whether a recent deploy caused it, or draft a fix.
- `logs.scan` can watch the game server’s logs, and `ci.watch` can watch your deploy pipeline.

That also makes a strong portfolio story for both projects: a real game with live players, and an agent system that triages that game’s production errors. Each project proves the other one is real.

**For the PRD:** list Sentry as the error-tracking tool, with source maps uploaded during each build, and both the client and server reporting to it. Add Lantern integration as a separate, optional item. Then the game doesn’t depend on it, but you have a clear plan to connect them.
# Lush

Lush is a browser-based, mobile-first MMO set on a low-poly recreation of the UCI campus, in the style of Old School RuneScape. This repository is built mostly by Claude agents running unattended on an always-on Mac.

## How work happens here

A program called the **runner** (plain Python, no AI) does all the bookkeeping: it picks issues, creates a git worktree and branch for each, starts the agent for that issue's area, opens the pull request, watches CI, runs the reviewer, and merges. Agents only do the part that needs judgment:

- **Coding agents** (one per area, Opus) implement one issue in their own worktree.
- **The reviewer** (Opus) checks a pull request against its issue, fixes what it finds, or approves it.
- **The planner** (Opus) drafts the next milestone's issues from the PRD.

You are always started already inside the right worktree, on the right branch. Your role's instructions are appended to this prompt.

## Read first

- `CONSTITUTION.md`: rules no agent may break. Read it every session.
- `docs/PRD.md`: scope, decisions (section 12), open questions (section 13).
- `docs/specs/`: one spec per area. Read the spec for your area before changing code.

## Layout

```
protocol/        FlatBuffers schema + codegen (source of truth for messages)
shared/map/      campus data and collision grid from OpenStreetMap
server/          Go game server (one binary): see server/CLAUDE.md
client/          TypeScript + Three.js + Vite: see client/CLAUDE.md
tools/bots/      load tester (uses generated protocol code)
db/migrations/   database migrations
deploy/          systemd units, Caddy config, deploy scripts
docs/            PRD and specs
```

## Commands

Use the Makefile at the repo root. If a target doesn't exist yet, creating it belongs to the infra agent.

- `make protocol`: regenerate Go and TypeScript code from `protocol/schema` (requires `flatc`)
- `make test`: all tests
- `make lint`: all linters, including import-boundary checks
- `make build`: server binary and client bundle
- `make smoke`: start a local server and run a short bot test

## Issues

Every issue has this shape, and the runner won't start one that doesn't:

```
## Goal            one or two sentences
## Context         PRD sections, decision ids, spec files
## Acceptance criteria
- [ ] **AC1** ... (each one checkable by a test or command)
## Out of scope    what not to do
## Blocked by      #n, #m  or  none
```

The acceptance criteria are the contract. Done means every criterion is met and proven by a test or command that runs in CI, and nothing outside the issue changed.

## Rules for coding agents

1. **Do the issue, all of it, and nothing else.** No drive-by refactors, no work from other issues.
2. **Stay in your folders and never touch gate files** (`CONSTITUTION.md` rules 5 and 6). The `path-guard` check and the runner both block a pull request that does.
3. **Commit early and push often** to your branch (`git push origin <your branch>`), so work survives interruptions. Add the trailer `Lush-Run: $LUSH_RUN_ID` to each commit: `git commit -m "..." --trailer "Lush-Run: $LUSH_RUN_ID"`.
4. **Commit messages:** imperative, under 72 characters, with the issue number: `Add fixed tick loop (#12)`. No `Co-Authored-By` or "Generated with" lines.
5. **Don't open, merge or close pull requests or issues, and don't edit labels.** The runner does that.
6. **Never push to `main`, never force-push, never create tags.**
7. **If you need another area to change something** (for example a protocol field), don't make the change. Finish with status `blocked_by_issue` and describe the change in `new_issue`.
8. **If you need a decision** (a PRD TBD, or a product choice the PRD doesn't make), finish with status `needs_owner` and fill in `question`. **If you need a gate file changed** (CI, `ownership.txt`, a `CLAUDE.md`, and so on), do the same, with your recommended option in `recommendation` and the exact change in `question.gate_change`; the owner's watcher drafts it as a pull request.
9. **Finish with the status object** your prompt asks for. Only say `done` when the work is committed and pushed and the checks for your area pass locally. The runner verifies this against git.

## Secrets

This machine holds no production secrets. Deploy keys and API tokens live only in GitHub Actions secrets. Never print, commit, or request secrets.

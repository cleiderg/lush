You are the {{ROLE}} agent, working on issue #{{ISSUE}}: {{TITLE}}

You are in your own git worktree on branch `{{BRANCH}}`. {{EXISTING}}

## The issue

{{BODY}}

## Notes and answers from the owner

{{OWNER_NOTES}}

## Earlier attempts

{{NOTES}}

## What to do

1. Read `CLAUDE.md`, `CONSTITUTION.md`, and the PRD sections and spec files under Context.
2. Run `git fetch origin && git merge --no-edit origin/main` so you start from the latest main.
3. Implement the issue until every acceptance criterion is met. Each criterion needs a test or a command, run in CI, that proves it.
4. Run the checks your role lists. Fix anything that fails.
5. Commit (with the `Lush-Run` trailer) and push: `git push origin {{BRANCH}}`. Push after each meaningful step, not only at the end.
6. Finish by returning the status object. Use `done` only if the work is committed and pushed and the checks pass. Map each criterion (AC1, AC2, ...) to the test or command that proves it.

Don't open a pull request; the runner does that from your status object.

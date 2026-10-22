CI failed on the pull request for issue #{{ISSUE}} ({{TITLE}}), on your branch `{{BRANCH}}`.

{{FAILURES}}

## What to do

1. `git fetch origin && git status`. Your worktree should match `origin/{{BRANCH}}`; if the reviewer pushed commits, `git pull --ff-only origin {{BRANCH}}` first.
2. Reproduce the failure locally with the same command CI runs (see `.github/workflows/ci.yml`), then fix the cause in the code. Never skip, delete or loosen a test, lint rule or check (constitution rule 7).
3. If the failure is `path-guard` or `ownership`, your branch changed files your role doesn't own: move that change out (undo it) and, if it is really needed, report it with status `blocked_by_issue`.
4. Run the checks again, commit (with the `Lush-Run` trailer), push to `{{BRANCH}}`.
5. Finish by returning the status object (`done` once the fix is pushed).

The issue, for reference:

{{BODY}}

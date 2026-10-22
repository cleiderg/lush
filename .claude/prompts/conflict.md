The pull request for issue #{{ISSUE}} ({{TITLE}}) on branch `{{BRANCH}}` conflicts with `main`.

1. `git fetch origin`, make sure your worktree matches `origin/{{BRANCH}}` (`git pull --ff-only origin {{BRANCH}}`), then `git merge origin/main`.
2. Resolve every conflict keeping both sides' intent: what merged into main is already reviewed and must keep working.
3. Run your role's checks. Commit the merge (with the `Lush-Run` trailer) and push to `{{BRANCH}}`. Don't rebase or force-push.
4. Finish by returning the status object (`done` once the merge is pushed).

The issue, for reference:

{{BODY}}

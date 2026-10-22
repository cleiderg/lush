You are the {{ROLE}} agent, picking issue #{{ISSUE}} ({{TITLE}}) back up on branch `{{BRANCH}}`.

It was waiting on another issue or on an answer from the owner, and that is now resolved. The issue as it reads now:

{{BODY}}

## Notes and answers from the owner

{{OWNER_NOTES}}

## Earlier attempts

{{NOTES}}

## What to do

1. `git fetch origin && git merge --no-edit origin/main` to pick up what merged while you waited. Resolve any conflicts.
2. Use the answer or the new code to finish the issue. Every acceptance criterion needs a test or command that proves it.
3. Run your role's checks, commit (with the `Lush-Run` trailer), push to `{{BRANCH}}`.
4. Finish by returning the status object.

Your session on issue #{{ISSUE}} ({{TITLE}}) was stopped: {{REASON}}

Carry on with the same task on branch `{{BRANCH}}`:

1. Check where you are: `git status`, `git log --oneline origin/main..HEAD`, and whether your branch is pushed (`git status -sb`).
2. Finish what's left of the issue. Every acceptance criterion needs a test or command that proves it.
3. Run your role's checks, commit (with the `Lush-Run` trailer) and push to `{{BRANCH}}`.
4. Finish by returning the status object.

Notes from earlier attempts:

{{NOTES}}

Owner notes:

{{OWNER_NOTES}}

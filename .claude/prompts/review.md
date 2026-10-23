Review pull request #{{PR}} for issue #{{ISSUE}}: {{TITLE}}

- Branch: `{{BRANCH}}` (role: {{ROLE}}), checked out in this worktree
- Head to review: `{{HEAD}}`
- Round {{ROUND}} of {{MAX_ROUNDS}}. CI is green on this head.

## The issue (the contract)

{{BODY}}

## Owner notes

{{OWNER_NOTES}}

## Earlier rounds

{{EARLIER}}

## Steps

1. Confirm `git rev-parse HEAD` prints `{{HEAD}}`. If it doesn't, change nothing and return verdict `needs_owner` with a question explaining the mismatch.
2. Read the change: `git diff origin/main...HEAD` and `git log --oneline origin/main..HEAD`. Read `CONSTITUTION.md`, the PRD sections and specs under Context, and the surrounding code you need.
3. **Criteria, one by one.** For each acceptance criterion (AC1, AC2, ...), find the test or command that proves it and run it. Status `met` with that evidence, or treat it as a blocker finding.
4. **Checklist.** Go through each and note findings with severities as your role defines them:
   - Scope: only what the issue asks; nothing listed under Out of scope; no unrelated changes.
   - Constitution rules 1 to 11, especially economy (1, 3), server authority (2), TBDs (4), ownership and gate files (5, 6), weakened checks (7), boundaries (8).
   - Correctness: edge cases a player would hit, disconnects mid-action, concurrency (only the tick mutates world state), resource limits per connection.
   - Tests: they would fail if the feature broke; not just compile-checks.
   - Performance budgets in PRD section 8 for anything per tick or per player.
   - Security: input validation, no secrets, no unbounded memory.
5. **If there are blocker or major findings:** fix them all as your role describes, re-run the area's checks, commit, push to `{{BRANCH}}`, and return verdict `fixed`. Mark each finding `fixed: true` or explain why not.
6. **If there are none:** change nothing and return verdict `pass` with `reviewed_head` set to `{{HEAD}}`. Minor findings may be listed; they don't block.
7. **If a decision only the owner can make blocks the PR:** return `needs_owner` with the question. If the fix needs a gate file changed, put the exact change in `question.gate_change`.

Your final answer is the verdict object. `reviewed_head` is always `{{HEAD}}`.

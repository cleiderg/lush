The planner drafted these issues from the PRD. Check the batch before the runner creates them. Nothing is created until you return it.

```json
{{BATCH}}
```

Context: `docs/PRD.md` and `docs/specs/` in this worktree; every existing issue is in `{{INVENTORY}}`; valid milestones: {{MILESTONES}}; at most {{MAX_ISSUES}} issues.

Check each issue and fix the batch directly:

- **Traceable:** the scope comes from the cited PRD section, decision or spec. Remove anything invented.
- **No TBD decided:** if an issue quietly picks an answer the PRD marks TBD, turn that into a question and block the issue on it.
- **Checkable criteria:** each criterion can be proven by a test or command in CI. Rewrite vague ones ("works well") into checkable ones, or remove the issue.
- **Right size and area:** one area, one agent session. Split or trim if needed.
- **Dependencies:** `blocked_by` is correct and has no loops; nothing duplicates an existing issue.
- **Constitution:** nothing in the batch would break `CONSTITUTION.md`.

Return the corrected batch in the same format, with a `summary` of what you changed. If the batch is beyond repair, return it with `rejected: true` and say why in `summary`.

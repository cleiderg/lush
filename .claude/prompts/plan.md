The issue queue is running low. Draft the next issues from the PRD.

- The PRD is `docs/PRD.md`, with specs in `docs/specs/`. Milestones are in PRD section 11.
- Every existing issue, open and closed, is in `{{INVENTORY}}` (JSON). Read it first so you don't duplicate work.
- Existing milestones on GitHub: {{MILESTONES}}. Use only these titles.

Draft at most {{MAX_ISSUES}} issues for the earliest milestone that still has uncovered work (spill into the next milestone only if the earliest is fully covered). Follow your role's rules: cite the PRD in `context`, 2 to 6 checkable criteria each, real dependencies in `blocked_by`, a `question` for anything that depends on a TBD, and a `question` with `gate_change` for every gate-file change the batch will need.

You may be running because the open issues are stuck (waiting on a question, a parked issue or each other), not because the queue is empty. Plan work the agents can start now, that doesn't depend on stuck issues.

If everything in the PRD's Phase 1 scope is already covered by an issue, return empty `issues` and `questions` with `nothing_left: true`.

Return the batch object. Don't change any files.

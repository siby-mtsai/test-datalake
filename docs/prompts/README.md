# Claude Code prompts

Per the brief (Sections 1 and 13): any Claude Code prompt used for this project is committed here
*before* it's run, on the feature branch. Results are verified against live behaviour (a
successful Athena query, a matching reconciliation report) rather than accepted on green unit
tests alone.

Naming convention: `NNN-short-description.md`, numbered in the order they were run, one file per
prompt. Include enough context in the file to reproduce the run (what phase/step it served, what
it was expected to produce).

No prompts have been run yet — the first is expected to be the Phase 0 discovery script once
Postgres access is available (see [`../STATUS.md`](../STATUS.md) for current blockers).

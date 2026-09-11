# Re-run runbook

TODO: document during Phase 2. Export runs are idempotent, keyed by `(table, event_date)` —
re-running a completed key replaces the Iceberg partition rather than duplicating it (brief
Section 7, Phase 2, step 2). Document the operator steps to trigger a manual re-run and how to
confirm reconciliation passed afterward.

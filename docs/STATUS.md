# Status

Progress log per the brief's working rhythm (Section 13): updated at the end of each phase.

## Phase 0 — Discovery (target: 3 working days)

**Status:** Not started — blocked on access.

### Blockers

- **AWS access to Dev account** (`517293881120`, `miracletraffic-india-dev`): AWS CLI has a
  `default` profile configured locally, but every call fails SSL certificate verification
  (`SSL: CERTIFICATE_VERIFY_FAILED`). Looks like a local/corporate proxy CA issue rather than a
  permissions problem — needs to be fixed before any AWS calls (including Athena/Glue setup in
  Phase 1) will work from this machine.
- **Postgres (`mtsai-api`) read access**: no connection string, `.pgpass`, or DB driver available
  in this environment. Need either a read-replica connection string or a dedicated low-privilege
  role (per Section 4, "Source" row), plus a Postgres client to run the discovery queries with.
- **Confirm remote repo**: this local directory is not yet connected to a `mtsai-rnd/mtsai-datalake`
  GitHub remote. Need to confirm the remote exists (or should be created) before pushing.
- **Read replica question** (Section 7, Phase 0, step 4): need to confirm with the mtsai-api team
  whether a read replica exists and its lag, to decide the nightly export window.

### What's ready once access lands

- [`phase0-inventory-template.md`](phase0-inventory-template.md) — run against
  `pg_stat_user_tables` / `pg_total_relation_size`.
- [`phase0-top-queries-template.md`](phase0-top-queries-template.md) — run against
  `pg_stat_statements`.
- [`classification-table.md`](classification-table.md) — Section 5 table, ready to fill in and
  circulate for sign-off (Section 12, checkpoint 1).

## Phase 1 — Foundation

**Status:** Not started — repo/doc scaffolding only so far (module directories exist as stubs).
Blocked on the same AWS access issue above before any `terraform apply` can run in Dev.

## Phase 2 — Export pipeline

**Status:** Not started.

## Phase 3 — Governance and erasure

**Status:** Not started.

## Phase 4 — Postgres trim and promotion

**Status:** Not started.

# Erasure runbook

Per brief Section 9, rehearsed for real in Test (per the standing project-wide decision to treat
every phase's brief-defined "Dev" as "Test") during Phase 3 — implemented as `cmd/erasure`
(`export/cmd/erasure/main.go`), invoked manually via `aws ecs run-task` (no schedule attached; the
trigger is an external verified request, not a clock).

**Trigger:** a verified erasure request for an account or vehicle, received through the identity
service process.
**Input:** the blind index or hashed identifier, the jurisdiction, and the request reference.

1. Record the request reference in `export-manifests/erasure/{date}/{request_ref}.json` before
   touching data.
2. For each raw and curated table listed in the classification table as containing personal data,
   run `DELETE FROM table WHERE identifier = :id` in Athena. Capture rows affected per table.
   (Only `trip_events`/`trip_events_curated` actually exist in the lake so far — the
   classification table also names `accounts`, `anpr_camera_events`, `reward_ledger`, but none of
   those have been exported yet, so there's nothing there to erase.)
3. Tighten the table's vacuum retention (`ALTER TABLE ... SET TBLPROPERTIES
   ('vacuum_min_snapshots_to_keep'='1', 'vacuum_max_snapshot_age_seconds'='60')` — 60 is Athena's
   enforced floor, confirmed empirically; `0` is rejected outright), then run `VACUUM` — Athena's
   single statement covering what Spark exposes as two separate procedures,
   `expire_snapshots`+`remove_orphan_files` — so time travel can't resurface the rows.
4. Verify: `SELECT count(*)` per table returns zero for the identifier, **and** a `SELECT ... FOR
   VERSION AS OF <pre-erasure-snapshot-id>` against the snapshot captured just before the DELETE
   now fails outright ("Iceberg snapshot ID does not exists") rather than merely returning zero
   rows — this is what actually proves time travel is blocked, not just that the live table looks
   clean. Record results against the request reference.
5. Confirm Postgres erasure was performed by the identity service for the hot window, and that any
   Glacier-tier objects containing the rows have been rewritten or expired.

**Trade-off:** erasure with time travel disabled loses the ability to reproduce historic reports
containing that identifier. This is the accepted trade-off.

**Two real bugs found rehearsing this:**

1. Every Athena call across this whole project (`cmd/export`, `cmd/curate`, `cmd/compact`, and
   this job) never specified an Athena workgroup, defaulting to "primary" — which turned out to be
   serving a **stale cached Iceberg snapshot** (reproducibly undercounting a live table by 11
   rows; confirmed unrelated to Athena's query-result-reuse feature, which was tested explicitly
   disabled and still showed it). An explicit workgroup consistently saw correct, current data.
   Fixed by giving the pipeline its own dedicated Athena workgroup (`aws_athena_workgroup.pipeline`
   in `terraform/modules/export-task/main.tf`) and adding a `WorkGroup` field to
   `internal/lake.AthenaRunner`, set on every call across all four binaries. This also means every
   reconciliation/idempotency check earlier in this project that "matched" while querying via the
   unset default should be treated with appropriate caution — though in practice the actual DML
   (INSERT/DELETE) always appeared to commit correctly against the live table; only reads via the
   stale workgroup were affected.
2. An identifier with zero rows in a table to begin with (e.g. present in raw but not yet
   propagated to curated) made step 4's time-travel check fail: a `DELETE` matching zero rows
   doesn't create a new Iceberg snapshot, so the "pre-erasure" snapshot is just the unchanged
   current one, which correctly still resolves — the job was treating that as "time travel not
   blocked" when really there was nothing to hide in the first place. Fixed by short-circuiting
   `eraseFromTable` when the before-count is zero: that table is trivially, successfully done, not
   a failure. A production erasure request will routinely hit this (an identifier rarely has rows
   in every table at once), so this isn't an edge case worth skipping.

**Deployed under the real task role, not just local credentials with broader access** — this
matters because the IAM grants a job actually needs (S3 delete, the new workgroup, etc.) can only
be trusted once proven under the role that will really run it in production. First deployed run of
`cmd/compact` failed with `AccessDenied` listing the whole `athena-results/` zone (its `ListBucket`
grant is deliberately scoped to only its own `athena-results/export/` subfolder, not other
consumer workgroups' results — the same class of key-enumeration leak already fixed once in
Phase 1); fixed by narrowing the compact job's own ambition to match the already-correctly-scoped
permission, not by widening the grant.

## Rehearsal log

**2026-09-21, Test.** Identifier `hash_veh_000970` (attempt 1) and `hash_veh_000164`: rows falsely
reported as already zero — this was the stale-workgroup bug above, not a real absence; the DELETE
still landed correctly against the live table each time, confirmed after the fact via an
independent, correctly-workgrouped query. After fixing the workgroup issue:

**Identifier `hash_veh_000067`** (synthetic, from `tools/seed-api-db`'s `hash_veh_NNNNNN` pool),
jurisdiction `IN`, request ref `rehearsal-2026-09-21-004`:

| Table | Rows before | Rows after | Time travel blocked |
|---|---|---|---|
| `test_raw.trip_events` | 10 | 0 | Yes — pre-erasure snapshot `3011481733055550922` no longer resolves |
| `test_curated.trip_events_curated` | 1 | 0 | Yes — pre-erasure snapshot `2915178321408864835` no longer resolves |

**End-to-end duration: 28.03s** (brief step 1: "measure end-to-end erasure time for a single
identifier"). Independently re-confirmed zero rows on both tables via a fresh Athena query after
the fact, not just the job's own exit code. Manifest at
`export-manifests/erasure/2026-09-21/rehearsal-2026-09-21-004.json`.

**Deployed run, real task role, `hash_veh_001327`, request ref `rehearsal-2026-09-21-008`**: 10
rows in raw (erased, time travel blocked, 19.37s), 0 rows in curated (correctly handled as
"nothing to erase" after bug 2's fix, not a failure) — invoked via `aws ecs run-task` against the
deployed `mtsai-datalake-test-erasure` task definition, not a local binary. Independently
re-confirmed 0 rows via a fresh Athena query.

# Backfill runbook

Runs the deployed export job over a date range instead of a single date (brief Section 7, Phase 2
step 5: "run the job over the full history in date chunks. Record throughput."). No separate
infrastructure exists for this - it reuses the same ECS task definition, network config, and IAM
role the nightly schedule uses, invoked once by hand via `aws ecs run-task` with
`EXPORT_START_DATE`/`EXPORT_END_DATE` overriding the normal single-date `EXPORT_DATE`.

A "chunk" is one day - the job processes the range one date at a time (matching the Iceberg
partition granularity), continuing through any single date's failure so the whole range's
throughput can still be measured, but the run as a whole still fails (non-zero exit, triggers the
failure alarm) if any date failed.

## Running a backfill (Test)

```bash
aws ecs run-task \
  --region ap-south-1 \
  --cluster arn:aws:ecs:ap-south-1:690293068614:cluster/mtsai-datalake-test \
  --task-definition mtsai-datalake-test-export \
  --launch-type FARGATE \
  --network-configuration '{"awsvpcConfiguration":{"subnets":["subnet-0266f942977861ddd","subnet-08064e40f903eceab","subnet-05639de4c9e0732b7"],"securityGroups":["<export task security group id - terraform output export_task_security_group_id, or read from the task definition's current revision>"],"assignPublicIp":"ENABLED"}}' \
  --overrides '{"containerOverrides":[{"name":"export","environment":[{"name":"EXPORT_START_DATE","value":"2026-01-01"},{"name":"EXPORT_END_DATE","value":"2026-01-31"}]}]}'
```

Omit the `:<revision>` suffix on `--task-definition` to always use the latest deployed revision.

## Checking results

- CloudWatch Logs: `/mtsai-datalake/test/export`, log stream `export/export/<task-id>` - one
  `run <run-id>: table=trip_events date=...` / `run <run-id> succeeded: ...` pair per date, plus a
  final `backfill <start> to <end> complete: N succeeded, N failed, ...` summary line.
- Per-date manifests: `export-manifests/<date>/trip_events.json` in the lake bucket - identical in
  shape to a normal nightly run's manifest, one per date in the range.
- Aggregate manifest: `export-manifests/backfill/<start>_<end>/summary.json` - per-date row
  counts/checksums/durations plus totals and a success/failure count, for measuring throughput
  ("record throughput" per the brief).
- Re-running the same range is idempotent (same `DELETE` + `INSERT` per-partition mechanism as a
  normal run) - row counts should stay the same, not double.

## Caveat

Real `mtsai-api` history doesn't exist yet (Phase 0 is still blocked on that) - this backfills
whatever's seeded in `mtsai-api-sim`, the synthetic stand-in, not a real production-scale history.
Re-confirm this runbook's behavior against the real source once Phase 0 access exists.

# Compaction runbook

Per brief Section 4.2 (weekly Iceberg `OPTIMIZE`) and Section 7 Phase 3 step 2
(`expire_snapshots`/`remove_orphan_files` weekly). Implemented as `cmd/compact`
(`export/cmd/compact/main.go`), scheduled via EventBridge Scheduler
(`aws_scheduler_schedule.weekly_compact`, `cron(0 3 ? * SUN *)` UTC by default — Sunday 03:00 UTC,
a low-traffic window off the nightly export/curate path).

Runs `VACUUM <table>` via Athena against both `test_raw.trip_events` and
`test_curated.trip_events_curated` — Athena's single statement covering what Spark exposes as two
separate procedures, `expire_snapshots`+`remove_orphan_files`, governed by the table's
`vacuum_min_snapshots_to_keep`/`vacuum_max_snapshot_age_seconds` properties (left at Athena's
defaults for routine weekly hygiene — this job doesn't tighten them the way `cmd/erasure` does for
an actual deletion). `OPTIMIZE` (file compaction proper, for tables accumulating many small files)
isn't run yet — this v1 slice only handles the snapshot/orphan-file hygiene half; add an `OPTIMIZE
<table> REWRITE DATA` step here once file counts actually warrant it.

Also publishes the cost dashboard's S3-storage-by-prefix metric while it already has bucket read
access (`MTSAiDataLake/Storage`/`PrefixBytes`, dimensioned by `Prefix` — `raw`, `curated`,
`athena-results`, `export-manifests`) — piggybacked here rather than building a second scheduled
job just for a metric CloudWatch doesn't expose natively.

## Running manually

```bash
aws ecs run-task \
  --region ap-south-1 \
  --cluster arn:aws:ecs:ap-south-1:690293068614:cluster/mtsai-datalake-test \
  --task-definition mtsai-datalake-test-compact \
  --launch-type FARGATE \
  --network-configuration '{"awsvpcConfiguration":{"subnets":["subnet-0266f942977861ddd","subnet-08064e40f903eceab","subnet-05639de4c9e0732b7"],"securityGroups":["sg-03adcaeb0b6470eb3"],"assignPublicIp":"ENABLED"}}'
```

## Verified (2026-09-21, Test)

Ran locally against real Test AWS resources: both `VACUUM` statements succeeded on the first
syntax attempt, and all four `PrefixBytes` metrics published and independently confirmed via
`aws cloudwatch list-metrics --namespace MTSAiDataLake/Storage`.

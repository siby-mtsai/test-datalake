# module: export-task

Phase 1 (infra) / Phase 2 (job logic). Provisions the scheduled ECS Fargate task (ARM64) that runs
the Go export job, its IAM role (SELECT-only on the source Postgres schema via Secrets Manager,
no Postgres write access), the EventBridge Scheduler rule, and CloudWatch alarms/SNS topic for
success/failure and reconciliation mismatches (brief Sections 4 and 6).

Not yet implemented — blocked on AWS access (see [`../../../docs/STATUS.md`](../../../docs/STATUS.md)).

# 001: Let the consumer roles launch the existing erasure task (Test only)

- **Serves:** the mtsai-analytics local dashboard's "Erase" feature (outside this repo).
- **Expected output:** Terraform changes to `athena-workgroup`, `export-task` outputs and
  `envs/test`, plus input validation in `cmd/erasure`. Verified with fmt/validate/plan and
  `go test`, **not applied**. The user reviews and runs "Terraform Deploy" (test, apply) themselves.
- **Run on:** branch `feature/consumer-erasure-access`, 2026-09-25.
- **Concerns raised before running** (the user chose to go ahead as written): this gives analytics
  and forecasting, which are otherwise read-only, the ability to *trigger* deletion. It bypasses
  the brief §9 "verified request via the identity service" trigger. It is arguably a §12.6
  IAM-widening checkpoint. And `ecs:RunTask` container/role overrides can't be restricted by IAM.

---

# Task: let the analytics, forecasting and audit consumer roles launch the existing erasure task (Test only)

## Context
A local dashboard (mtsai-analytics) is getting an "Erase" feature. Users pick a consumer
(analytics / forecasting / audit). The dashboard then calls ecs:RunTask as that consumer's role
to run the existing, already-rehearsed erasure task (export/cmd/erasure, task definition family
mtsai-datalake-test-erasure). It passes ERASURE_IDENTIFIER, ERASURE_REQUEST_REF and
ERASURE_JURISDICTION as container environment overrides, then polls the task and reads the
erasure manifest from S3.

The erasure job itself does the deleting (raw + curated, VACUUM, time-travel check, manifest),
under its own task role. The consumer roles must NOT get any direct delete or write access to
the data.

Before running this prompt, commit it to docs/prompts/ on the feature branch (brief §1 / §13).

## Scope: Terraform only, test environment only
Work on a new branch: feature/consumer-erasure-access (branch from main).

1. terraform/modules/athena-workgroup/variables.tf
   - Add `erasure_access = optional(bool, false)` to the `consumers` object type.
   - Add module inputs, all defaulting to "" / [] so dev is unaffected:
     - erasure_task_definition_arn (string)
     - erasure_cluster_arn (string)
     - erasure_pass_role_arns (list(string)): the task role and the execution role

2. terraform/modules/athena-workgroup/main.tf
   Inside data "aws_iam_policy_document" "consumer_access", add dynamic statements that exist
   only when each.value.erasure_access is true and the inputs are non-empty:
   - sid "RunErasureTaskOnly": ecs:RunTask
     - resource: the erasure task-definition FAMILY with any revision, i.e.
       arn:aws:ecs:<region>:<account>:task-definition/mtsai-datalake-<env>-erasure:*
       (derive it from erasure_task_definition_arn by stripping the revision)
     - condition ArnEquals ecs:cluster = erasure_cluster_arn
   - sid "DescribeErasureTasks": ecs:DescribeTasks
     - resource: arn:aws:ecs:<region>:<account>:task/<cluster-name>/*
   - sid "PassErasureRolesToEcs": iam:PassRole
     - resources: erasure_pass_role_arns
     - condition StringEquals iam:PassedToService = ecs-tasks.amazonaws.com
   - sid "ReadErasureManifests": s3:GetObject
     - resource: arn:aws:s3:::<bucket>/export-manifests/erasure/*
   - s3:ListBucket on the bucket with condition s3:prefix = "export-manifests/erasure/*"
     (StringLike). Keep it scoped this way. Do not grant ListBucket on the whole bucket for this.
   The existing KMS Decrypt grant already covers reading the manifest. Do not add a new KMS key
   grant.

3. terraform/modules/export-task/outputs.tf
   - Add output "execution_role_arn" = aws_iam_role.execution.arn

4. terraform/envs/test/main.tf
   - Pass to module "athena_workgroup":
     - erasure_task_definition_arn = module.export_task.erasure_task_definition_arn
     - erasure_cluster_arn         = module.export_task.cluster_arn
     - erasure_pass_role_arns      = [module.export_task.task_role_arn, module.export_task.execution_role_arn]
     - consumers = the module's current defaults, copied exactly (same scan limits, budgets,
       raw_zone_read_access), with erasure_access = true on analytics, forecasting and audit.
   - Check this creates no dependency cycle between athena_workgroup and export_task.
   - Do not touch terraform/envs/dev.

5. export/cmd/erasure/main.go (small hardening, because input now comes from a UI)
   - loadConfig must reject ERASURE_IDENTIFIER unless it matches ^[A-Za-z0-9_]{1,128}$ (it is
     interpolated into SQL).
   - loadConfig must reject ERASURE_REQUEST_REF unless it matches ^[A-Za-z0-9._-]{3,64}$ (it
     becomes an S3 key).
   - If ERASURE_JURISDICTION is set, it must match ^[A-Z]{2}$.
   - Add a unit test for loadConfig covering valid and invalid values.
   - This changes the image, so note in the summary that the erasure image must be rebuilt and
     pushed before the new validation takes effect.

## Must not change
- No s3:DeleteObject, s3:PutObject on raw/ or curated/, or glue:Update*/Delete* for any consumer role.
- The consumer trust policies, workgroups, scan limits and budgets stay as they are.
- No ecs:RunTask on any other task definition, and no ecs:StopTask.

## Verify (do not apply)
- terraform fmt -check, terraform validate, and terraform plan in envs/test.
- The plan must show only in-place updates to the three aws_iam_role_policy.consumer resources,
  plus the new output. No replacements and no destroys. Paste the plan summary.
- go test ./... in export/.
- Do NOT run terraform apply, do NOT push, and do NOT trigger the Deploy workflow. Stop and
  report. I will review, then run "Terraform Deploy" (environment: test, action: apply) myself.

## After I apply: verification commands to give me (read-only)
Give me aws iam simulate-principal-policy commands for each of the three roles, showing:
- ecs:RunTask on mtsai-datalake-test-erasure is allowed
- ecs:RunTask on mtsai-datalake-test-export is denied
- s3:DeleteObject on curated/* and raw/* is denied
- iam:PassRole on some other role is denied

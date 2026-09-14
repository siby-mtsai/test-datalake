# Status

Progress log per the brief's working rhythm (Section 13): updated at the end of each phase.

## Phase 0 — Discovery (target: 3 working days)

**Status:** Not started — blocked on access.

### Blockers

- ~~AWS access to Dev account~~ **Resolved.** The `SSL: CERTIFICATE_VERIFY_FAILED` errors from
  AWS CLI, the Terraform provider plugin, and intermittently `git push` were all the same root
  cause: **Avast Antivirus's "Web/Mail Shield" HTTPS scanning** on this machine was
  man-in-the-middling all TLS connections and re-signing them with its own CA
  (`issuer=... CN=Avast Web/Mail Shield Root`). Terraform (Go) trusted it via the Windows system
  cert store; AWS CLI (Python/botocore) does not, hence the failures. Disabling Avast's HTTPS
  scanning fixed AWS CLI, `terraform validate`, and git access immediately — no CA bundle
  workaround needed. If this resurfaces on another machine, check for the same interception
  pattern before assuming an AWS permissions problem.
- **Postgres (`mtsai-api`) read access**: no connection string, `.pgpass`, or DB driver available
  in this environment. Need either a read-replica connection string or a dedicated low-privilege
  role (per Section 4, "Source" row), plus a Postgres client to run the discovery queries with.
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

**Status:** Applied and verified in Test. Dev is deprioritized (not in scope right now — see
below).

All four modules (`lake-bucket`, `glue-catalog`, `athena-workgroup`, `export-task`) are
implemented per Section 4/6/7. `terraform fmt`/`validate` are clean in all three roots
(`bootstrap/`, `envs/dev/`, `envs/test/`) now that the TLS issue above is resolved.

**Test account (690293068614) was applied via `deploy.yaml`** and manually verified end-to-end:
- Created an Iceberg table by hand in `test_raw`, confirmed a CTAS into `test_curated` worked.
- Ran the actual isolation test the brief's Phase 1 "done when" implies: assumed the
  `mtsai-datalake-test-forecasting` role directly (`aws sts assume-role`, not just switching
  Athena workgroups while signed in as an admin role — that doesn't test anything, since the
  workgroup selection doesn't change the calling identity) and confirmed its real S3 access.
- **Found and fixed a real bug**: `s3:ListBucket` was granted on the bare bucket ARN in the
  `CuratedZoneRead` and `AthenaResultsReadWrite` statements (and the equivalent
  `WriteRawAndManifests` statement in `export-task`), with no `s3:prefix` condition.
  `s3:ListBucket` is a bucket-level action — granting it unconditionally let every consumer role
  enumerate (list keys under) the *entire* bucket, including `raw/`, even though `s3:GetObject`
  was correctly scoped to `curated/*` only. So `forecasting` could list `raw/test_table/` (key
  enumeration leak) but still could not actually read its contents. Fixed in
  `terraform/modules/athena-workgroup/main.tf` and `terraform/modules/export-task/main.tf` by
  splitting each zone's access into a `GetObject`/`PutObject` statement (object-ARN scoped, as
  before) and a separate `ListBucket` statement scoped via an `s3:prefix` `StringLike` condition
  matching only that zone's prefix.
- **Re-applied and re-verified (2026-09-14)**: deployed policy confirmed to match the fix
  (`CuratedZoneGet`/`CuratedZoneList` split, `s3:prefix` condition present). Re-ran the
  assume-role check: `forecasting` now gets `AccessDenied` on `s3 ls raw/` (previously succeeded),
  while `s3 ls curated/` still works. Isolation between consumer roles is confirmed working for
  Test.

### CI: GitHub Actions (mirrors `mtsai-commuter-infra`'s pattern)

`.github/workflows/` now has `bootstrap.yaml`, `deploy.yaml`, and `deploy-locked.yaml` (see
`.github/workflows/README.md`), matching the workflow style already established in
`mtsai-commuter-infra` — including its state-backend naming convention
(`<project>-tfstate-<account_id>` bucket, `<project>-tflock` table), now reflected for real in
`terraform/envs/{dev,test}/versions.tf` (`mtsai-datalake-tfstate-517293881120` /
`mtsai-datalake-tfstate-690293068614`, both with lock table `mtsai-datalake-tflock`) and a new
`bootstrap/` module (root of the repo) that creates them. No more `TODO-*` bucket/table
placeholders — but the buckets/table don't exist yet, so `terraform init` will still fail until
`bootstrap.yaml` (or a local `terraform apply` in `bootstrap/`) is actually run once per account.
Already done for Test (`mtsai-datalake-tfstate-690293068614` / `mtsai-datalake-tflock` exist).

### Dev — deprioritized

Per explicit direction: **Dev is not being pursued right now** — all active work is on Test
(`690293068614`) only. `terraform/envs/dev/` stays scaffolded (fmt/validate clean) as a reference
but is not being applied, and isn't blocking anything. Revisit this section if that changes.

### Outstanding for Test

- Re-apply Test via `deploy.yaml` to roll out the `s3:ListBucket` fix above, then re-verify with
  the assume-role check.
- Populate `export_subnet_ids` / `export_security_group_ids` in
  `terraform/envs/test/variables.tf` once Test's VPC network is decided — the export-task
  module's `aws_scheduler_schedule` needs real subnets to target (currently skipped since both
  default to empty).
- Decide `postgres_secret_arn` and `alarm_email` once Phase 0 confirms DB access details.
- Confirm whether AWS Budgets / cost-allocation tags are activated org-wide — `athena-workgroup`'s
  `aws_budgets_budget` resources only get created once `alarm_email` (→
  `budget_notification_emails`) is set.

## Phase 2 — Export pipeline

**Status:** Not started.

## Phase 3 — Governance and erasure

**Status:** Not started.

## Phase 4 — Postgres trim and promotion

**Status:** Not started.

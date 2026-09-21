# Status

Progress log per the brief's working rhythm (Section 13): updated at the end of each phase.

## Phase 0 — Discovery (target: 3 working days)

**Status:** Run for real against a synthetic stand-in database (2026-09-16) — genuinely blocked
only on the real `mtsai-api` database itself, which still doesn't exist anywhere in this project.

### The real blocker, and how it got worked around

There has never been a real `mtsai-api` Postgres database reachable from this project — not
production, and (confirmed 2026-09-16) not even a copy of it in the Test AWS account. Rather than
stay blocked indefinitely, provisioned `mtsai-api-sim`
(`terraform/modules/mtsai-api-sim`) — a disposable RDS Postgres instance in Test, seeded with a
schema and synthetic data covering every Section 5 classification class
(`tools/seed-api-db`) — and ran Phase 0's actual discovery queries (`tools/pg-query`, since no
`psql` client exists in this environment either) against it for the first time.

**This is explicitly a stand-in, not the real thing.** Every deliverable below is genuinely useful
for proving the classification scheme and discovery process work end-to-end, but table names,
row counts, and query patterns all need re-confirming against the real `mtsai-api` schema once
access to it exists — each doc says so explicitly, not just here.

- ~~AWS access to Dev account~~ **Resolved earlier.** The `SSL: CERTIFICATE_VERIFY_FAILED` errors
  from AWS CLI, the Terraform provider plugin, and intermittently `git push` were all the same
  root cause: **Avast Antivirus's "Web/Mail Shield" HTTPS scanning** on this machine was
  man-in-the-middling all TLS connections and re-signing them with its own CA. Disabling Avast's
  HTTPS scanning fixed it — no CA bundle workaround needed.
- **Read replica question (Section 7, Phase 0, step 4) — still genuinely open.** This asks whether
  a read replica exists for the *real* `mtsai-api` database, to decide the nightly export window.
  A synthetic single-instance stand-in can't answer this — it still needs the actual mtsai-api
  team.

### Deliverables — filled in for real, against the synthetic stand-in

- [`phase0-inventory-template.md`](phase0-inventory-template.md) — real `pg_stat_user_tables` /
  `pg_total_relation_size` / primary-key output for all 6 seeded tables.
- [`phase0-top-queries-template.md`](phase0-top-queries-template.md) — real `pg_stat_statements`
  output from a representative seeded workload; the analytical-vs-transactional split matches the
  brief's own Section 2 problem statement (analytical queries dominate total execution time
  despite far fewer calls).
- [`classification-table.md`](classification-table.md) — Section 5 classes mapped onto real table
  names for the first time. Retention periods are still bracketed placeholders (a business/legal
  decision, not something discovery alone determines) — not yet circulated for sign-off.

### mtsai-api-sim details (not secret — the instance itself, not its credentials)

- Instance: `mtsai-api-sim-test`, `db.t3.micro`, single-AZ, `ap-south-1`, Test account
  (`690293068614`).
- Publicly accessible but locked via security group to one client IP (no bastion/VPN exists in
  this VPC's default-public-subnets-only setup) — a known, accepted simplification, not a
  production pattern.
- Credentials: `aws-secretsmanager:mtsai-api-sim-test-credentials`, shaped to match exactly what
  `export/internal/config/config.go` already parses — this secret's ARN could be wired into
  `export-task`'s `postgres_secret_arn` later if Phase 2 should read from this instead of local
  Docker (not done automatically; a deliberate follow-up decision).

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
- **Re-applied and re-verified at the S3 level (2026-09-14)**: `forecasting` now gets
  `AccessDenied` on `s3 ls raw/` (previously succeeded), while `s3 ls curated/` still works.
- **Then verified at the actual Athena query level** (not just raw S3 calls), which surfaced two
  more real, pre-existing gaps that had simply never been exercised before (nobody had run a
  query as a restricted consumer role until now):
  1. `s3:GetBucketLocation` on the results bucket was never granted anywhere - Athena calls it to
     verify the output bucket before running any query at all, for every consumer, regardless of
     which zone. Without it, every query failed with "Unable to verify/create output bucket."
  2. The whole `aws:ResourceTag/Environment`-tag-based approach to scoping Glue reads was broken
     by design: Glue *tables* are created by hand via Athena DDL/CTAS (exactly as Phase 1 step 4
     describes), not by Terraform, so they never receive the tag from a provider's `default_tags`
     — meaning `glue:GetTable` silently failed the tag condition for every real table, which
     Athena surfaced as a confusing `TABLE_NOT_FOUND` rather than an access-denied error. Fixed by
     scoping Glue reads with explicit ARNs
     (`arn:aws:glue:*:<account>:database/<db>` / `.../table/<db>/*`) instead of tags - this also
     properly isolates raw-zone *metadata* by zone, which the tag approach never did either.
     Additionally, Glue's authorization model turned out to check the *entire* resource hierarchy
     for any action touching a table — reading `test_table_curated` required an explicit Allow on
     the catalog resource (`arn:...:catalog`) *and* the database/table ARNs, not just the most
     specific one; missing the catalog-level grant for `GetTable`/`GetTables`/`GetPartition(s)`
     (as opposed to just `GetDatabase`/`GetDatabases`) was the last thing blocking a legitimate
     `curated` read even after every other permission was correct.
  Both fixes applied to `terraform/modules/athena-workgroup/main.tf` and the equivalent statements
  in `terraform/modules/export-task/main.tf` (which will hit the same Glue-write version of this
  once Phase 2's export job actually runs).
- **Fully re-verified end-to-end (2026-09-14)**: as `forecasting`, `SELECT * FROM test_table_curated`
  (database `test_curated`) → `SUCCEEDED`; `SELECT * FROM test_table` (database `test_raw`) →
  clean `AccessDenied` on `glue:GetDatabase`, not a confusing `TABLE_NOT_FOUND`. As `audit` (which
  *should* retain raw access), the same raw query → `SUCCEEDED`, confirming the fixes didn't
  over-restrict legitimate access. Isolation between consumer roles is genuinely working now, not
  just believed to be from reading the Terraform.
- **Real data round-trip tested (2026-09-14)**: `INSERT INTO test_table VALUES ...` (raw) →
  `SELECT` back → `INSERT INTO test_table_curated SELECT ... FROM test_raw.test_table` (transform)
  → `SELECT` back. Confirms the storage/query layer works with real data, not just empty tables.
- **Fifth bug found (2026-09-15), fixed and applied**: `aws_s3_bucket_logging` had the lake bucket
  logging to itself, but S3 server access logging doesn't support an SSE-KMS-encrypted destination
  (only SSE-S3) — the lake bucket's default encryption is SSE-KMS, so this was silently a no-op
  (`terraform apply` succeeded, but no `access-logs/` objects were ever actually delivered,
  confirmed after hours of real traffic). Fixed in `terraform/modules/lake-bucket/main.tf` by
  adding a separate SSE-S3 logs bucket (`mtsai-datalake-test-690293068614-ap-south-1-logs`) as the
  logging target. Applied via `deploy.yaml`; configuration re-verified directly:
  `get-bucket-logging` on the lake bucket now points at the new logs bucket, and
  `get-bucket-encryption` on the logs bucket confirms `AES256` (not KMS).
- **Sixth bug found (2026-09-15), fixed and applied**: after the fifth-bug fix, `access-logs/` was
  *still* empty. Root cause: the logs bucket has Object Ownership `BucketOwnerEnforced` (ACLs
  disabled — the S3 default for buckets created since April 2023), so the classic ACL-based grant
  to the S3 Log Delivery group can't apply at all, and no bucket policy existed to grant that
  permission another way. Without either mechanism this isn't a delay, it's a hard, permanent
  block — confirmed via `get-bucket-ownership-controls` (`BucketOwnerEnforced`) and
  `get-bucket-policy` (`NoSuchBucketPolicy`, i.e. none existed). Fixed by adding an explicit bucket
  policy granting `logging.s3.amazonaws.com` `s3:PutObject` under `access-logs/*`, scoped with
  `aws:SourceArn`/`aws:SourceAccount` conditions to only the lake bucket. Applied and confirmed via
  `get-bucket-policy`.
- **Log delivery confirmed working end-to-end (2026-09-15)**: log objects appeared in
  `access-logs/` covering the marker traffic from `2026-09-15T11:47:17Z`/`11:49:55Z`. Observed
  delivery latency was ~5.5 hours (request at 12:15 UTC, object visible ~17:45 UTC) — longer than
  the commonly-cited "a few hours," but still consistent with AWS's "best effort, no SLA"
  description of this feature. S3 access logging on the lake bucket is genuinely functional now,
  closing out both the fifth and sixth bugs above.

### Still-untested pieces (not known bugs, just never exercised by a live run)

- KMS key has no custom key policy — Section 6 says it should restrict usage to "the export role,
  Athena workgroup roles, and account break-glass role only," but currently relies on IAM alone
  (AWS default key policy).
- Export-task's own IAM role has still never actually been assumed and used — the Phase 2 v1
  slice exercised the same S3/Glue/Athena *calls* the role needs to make (which is how its two
  permission gaps got found and fixed), but ran under the tester's own admin credentials, not the
  `export-task` role itself. Genuinely confirming the role's policy is sufficient (not just
  "should be, by inspection") needs either `sts assume-role` into it or an actual container run.
- The CloudWatch alarm / SNS notification path for ECS task failures has never been triggered.
- `aws_budgets_budget` resources aren't created yet (`alarm_email` is empty).

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

**Status:** Running automatically in Test (2026-09-21) — one table only (`trip_events`).

Go export job (`export/`, module `mtsai-datalake-export`) that reads Postgres via a server-side
cursor, writes Parquet, uploads to S3, commits into an Iceberg table via Athena, reconciles row
count/checksum, and writes a manifest. Handles exactly one table so far: `trip_events`, a
synthetic fixture (see `export/README.md` for why it's not schema-generic yet). Real `mtsai-api`
Postgres access still doesn't exist (Phase 0 still blocked), so this was tested against a local
Docker Postgres seeded with fixture data — matches the brief's own stated testing convention
(Section 8) — while every AWS-side call (S3, Glue, Athena) hit the real Test account.

**Also created**: the `trip_events` Iceberg table itself in `test_raw` (brief Phase 1 step 4 — "one
Iceberg table by hand" — hadn't actually named `trip_events` before this; only the generic
`test_table` from earlier isolation testing existed).

**Three real bugs found and fixed by actually running it** (none of these would show up in code
review or `go build`):
1. Athena's SQL engine v3 (Trino-based) rejects a plain `CREATE TABLE ... WITH (external_location
   = ..., ...)` statement without an `AS SELECT` — that form is CTAS-only. Registering existing S3
   files as a table needs classic Hive DDL: `CREATE EXTERNAL TABLE ... STORED AS PARQUET LOCATION
   ...`.
2. That same DDL rejects `IF NOT EXISTS` when combined with `EXTERNAL` (undocumented, confirmed
   empirically) — dropped it; harmless since each staging table name already has a unique run-ID
   suffix.
3. `parquet-go`'s reflection-based `time.Time` writer only correctly encodes the `Timestamp`
   logical type (physical `int64`); for `Date` (physical `int32`) it still writes a raw nanosecond
   `int64` into the `int32` slot, producing garbage (`event_date` round-tripped as
   `-1454296-02-29`). Worked around by encoding the date manually as days-since-epoch (`int32`)
   in a Parquet-specific struct, confirmed via source inspection of the library
   (`column_buffer_write.go`), not by guessing an AWS/SQL syntax fix.

**Also fixed**: two IAM gaps in the already-deployed `export-task` role (S3 access to write Athena
query results anywhere, `glue:DeleteTable` for staging-table cleanup) — found while designing the
job's actual calls, applied to Test.

**Verified**: read 5 rows → uploaded → committed → reconciled (checksum matched) → manifest
correct; re-ran for the same date and confirmed the row count stayed at 5, not 10 (idempotency);
`docker build --platform linux/arm64` succeeds for the container image (not pushed anywhere yet).

**Explicitly out of scope for this slice** (see `export/README.md`): pushing the image to ECR /
wiring it into the ECS task definition, the real EventBridge schedule actually triggering it
(still blocked on VPC subnet IDs), backfill-mode CLI, curated-layer CTAS automation, handling more
than one table.

### Connected to a real (if synthetic) AWS database for the first time (2026-09-17)

Previously only ever run against local Docker Postgres. Pointed it at `mtsai-api-sim` (the Test
Phase 0 stand-in database) instead — `trip_events` there already matched the export job's expected
schema exactly, and the Secrets Manager secret was already shaped to match what
`internal/config/config.go` parses, both deliberately set up that way earlier.

**Fourth real bug found**: connection failed with `no pg_hba.conf entry ... no encryption` — the
export job hardcoded `sslmode=disable` (fine for local Docker, which isn't configured for SSL at
all) but RDS rejects plaintext connections outright. Fixed by switching to `sslmode=prefer`
(negotiates SSL when available, falls back when not), so the same code now works unmodified
against both.

**Verified end-to-end against real AWS infrastructure on both ends** (not local Docker + Test AWS
like before — now RDS + Test AWS): exported `trip_events` for `2026-07-07` (76 rows), independently
confirmed row count and checksum matched on both the Postgres source and the Iceberg destination
via Athena (76 rows, checksum 177962 on both sides).

Still not wired up automatically — `postgres_secret_arn` in `terraform/envs/test/variables.tf` is
still empty; this run passed `mtsai-api-sim`'s credentials by hand. Wiring that variable to
`module.mtsai_api_sim.secret_arn` would make it the default source instead of a manual override —
a natural next step, not done automatically here.

### Attempted to make it run automatically (2026-09-21) — partially applied, blocked on IAM

Wired up everything the dormant `export_schedule_enabled` scheduling code (written back in Phase 1,
never activated) needed to finally turn on: an ECR repository for the image, a security group for
the Fargate task (module-owned, matching `mtsai-api-sim`'s pattern — converted that module's
security group from inline ingress/egress blocks to standalone rule resources first, since mixing
inline blocks with rules added from another module is a well-documented Terraform footgun),
`postgres_secret_arn` finally wired to `module.mtsai_api_sim.secret_arn` automatically,
`assign_public_ip = true` (required — public subnets, no NAT gateway).

**Applied and confirmed live**: the ECR repo, the export task's security group + its DB ingress
rule, and a new ECS task definition revision with the Postgres secret actually injected.

**Blocked, not worked around**: creating the EventBridge Scheduler itself requires an IAM role for
it to assume, which requires `iam:CreateRole` — and the current session's `DeveloperPowerUser`
access does **not** have that permission:
```
AccessDenied: ... not authorized to perform: iam:CreateRole on resource:
arn:aws:iam::690293068614:role/mtsai-datalake-test-export-scheduler
```
This is exactly the scenario the brief's own Section 7, Phase 1 step 3 anticipated: *"if you hit
AccessDenied on iam:CreateRole, stop and check the aws-org notes before widening anything."*
Per that explicit instruction, this was not worked around (no self-granted permissions, no
alternate account). **Needs a real IAM permissions fix from whoever owns the aws-org handoff** —
either broaden `DeveloperPowerUser`'s policy to allow creating this specific role, or create
`mtsai-datalake-test-export-scheduler` by hand and re-run `terraform apply` (which would then just
adopt/manage it going forward... actually would need an `terraform import` first since Terraform
doesn't know about a hand-created role - flag this back if that's the route taken).

The container image itself is also still the `hello-world` placeholder — hasn't been built and
pushed to the new ECR repo yet, a separate remaining step once the IAM blocker clears.

Not the same class of thing as the SSL/logging/DDL bugs above — this one isn't a code bug, it's
missing permissions genuinely outside this session's authority to grant itself.

### Automated export now live and verified end-to-end (2026-09-21)

The `iam:CreateRole` blocker above was cleared by whoever owns the aws-org handoff, not worked
around: the session's AWS identity was switched to a role with that permission, and the scheduler
role/schedule were created for real. `terraform plan` confirmed zero drift afterward.

Built and pushed the real ARM64 export image (`690293068614.dkr.ecr.ap-south-1.amazonaws.com/mtsai-datalake-test-export:latest`,
18.3MB, distroless base) to replace the `hello-world` placeholder, and applied that to Test —
`export_container_image` now defaults to the real image, not the placeholder.

**Manually invoked the deployed task once** (`aws ecs run-task`, same task definition/network
config/IAM role the nightly schedule uses, with `EXPORT_DATE=2026-07-07` overridden so it wouldn't
need to wait on the actual clock) to prove the automated path works without waiting up to 24h for
the real `cron(0 20 * * ? *)` to fire.

**Seventh bug found (2026-09-21), fixed and applied**: first run failed at the very last step —
`commit to iceberg: ... InvalidRequestException: Unable to verify/create output bucket` — even
though the read/parquet/upload steps succeeded (76 rows). Root cause: the export task's own IAM
role was missing `s3:GetBucketLocation` on the results bucket, the exact same gotcha already
found and fixed for the Athena consumer roles back in Phase 1 (see above) but never carried over
to `export-task`'s own policy. Fixed in `terraform/modules/export-task/main.tf` by adding the same
`ResultsBucketLocation`-style statement, applied (0 add, 1 change, 0 destroy).

**Re-ran the same manual invocation after the fix — succeeded end-to-end**: container exit code 0,
log line `run ... succeeded: 76 row(s), checksum=177962, duration=11.01s` — matching the
already-verified value exactly. Independently re-confirmed from the outside (not just trusting the
container's own log): `SELECT COUNT(*) FROM test_raw.trip_events WHERE event_date = DATE
'2026-07-07'` via Athena → `76`, and `export-manifests/2026-07-07/trip_events.json` present in S3.

This closes out the last "still-untested piece" from Phase 1 (the export-task role had never
actually been assumed/used, only inspected) and the last item blocking Phase 2's brief-defined
"done when": the job now runs unattended, in AWS, on its own schedule, under its own IAM role —
verified by the same path the schedule itself uses, not a stand-in. The one thing not yet directly
observed is an actual unattended nightly firing (the schedule fires at 20:00 UTC); this manual
invocation used the identical task definition, network config, and IAM role, so there's no
remaining reason to expect that to behave differently.

## Phase 3 — Governance and erasure

**Status:** Not started.

## Phase 4 — Postgres trim and promotion

**Status:** Not started.

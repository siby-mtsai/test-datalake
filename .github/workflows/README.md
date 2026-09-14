# .github/workflows

Mirrors the pattern already used in `mtsai-commuter-infra` (same repo layout, same workflow
style), adapted for this project having two environments (dev, test) instead of one (prod).

- **`bootstrap.yaml`** — creates the S3 bucket + DynamoDB table each environment's Terraform
  state lives in. Run this once per account before `deploy.yaml`/`deploy-locked.yaml` can
  `terraform init` for real. Has no remote backend of its own (it creates the backend), so its
  own state round-trips through a workflow artifact between runs — a local `terraform apply` from
  `bootstrap/` is more robust if you have credentials to run it that way instead.
- **`deploy.yaml`** ("normal") — runs immediately when triggered. Pick `environment`
  (`dev`/`test`) and `action` (`apply`/`destroy`) from the Actions tab.
- **`deploy-locked.yaml`** ("locked") — identical, but the job runs under a
  `<environment>-locked` GitHub Environment instead of the plain one, so it can require manual
  approval before `apply`/`destroy` actually executes. Use this one for anything destructive.

All three authenticate to AWS via OIDC (no long-lived AWS keys in CI) — see
`aws-actions/configure-aws-credentials` in each file.

## Required setup before any of these can actually run

1. **OIDC provider + IAM role per account**: role name is `<environment>-datalake` — confirmed
   for Test (`arn:aws:iam::690293068614:role/test-datalake`); Dev's is assumed to follow the same
   pattern (`arn:aws:iam::517293881120:role/dev-datalake`) but not yet confirmed. Each needs to
   trust this repo's GitHub OIDC provider, with permissions to manage the resources in
   `bootstrap/` and `terraform/modules/*`.
2. **Run `bootstrap.yaml` (or `bootstrap/` locally) once per account** so
   `mtsai-datalake-tfstate-<account_id>` and `mtsai-datalake-tflock` actually exist — until then,
   `terraform init` in `terraform/envs/dev|test` will fail (the backend in `versions.tf` points at
   those exact names already).
3. **GitHub Environments**: create `dev`, `test`, `dev-locked`, `test-locked` under repo
   Settings → Environments. Add required reviewers to the `*-locked` ones only — that's what makes
   `deploy-locked.yaml` actually pause for approval; the plain ones should stay unprotected so
   `deploy.yaml` runs immediately.
4. `export_subnet_ids` / `export_security_group_ids` / `postgres_secret_arn` / `alarm_email` in
   each env's `terraform/envs/<env>/variables.tf` still default to empty — override with `-var`
   flags or update the defaults once those are confirmed (brief Section 7 Phase 0/1).

## Still TODO

- Plan-on-PR / apply-on-merge automation (brief Section 8's actual wording) — not built yet;
  today it's all manual `workflow_dispatch`.

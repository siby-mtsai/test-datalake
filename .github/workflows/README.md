# .github/workflows

Brief Section 8 calls for "plan on PR, apply on merge, OIDC to the target account." What's
implemented so far is a manual-dispatch pair instead (plan-on-PR is still TODO — see below), both
running `terraform` against `terraform/envs/{dev,test}/` via AWS OIDC (no long-lived AWS keys in
CI):

- **`terraform-deploy.yml`** ("normal") — runs immediately when triggered. Pick an environment
  (`dev`/`test`) and an action (`apply`/`destroy`) from the Actions tab.
- **`terraform-deploy-locked.yml`** ("locked") — identical inputs, but the job runs under a
  `<environment>-locked` GitHub Environment instead of the plain one, so it can require manual
  approval before `apply`/`destroy` actually executes.
- **`_terraform-run.yml`** — the shared reusable workflow both of the above call into (init,
  validate, plan, upload the plan as an artifact, apply). Not meant to be triggered directly.

## Required setup before either workflow can actually run

1. **OIDC trust + IAM role per account** (Section 7, Phase 1 step 3): each target account
   (Dev `517293881120`, Test `690293068614`) needs an IAM role named
   `github-actions-terraform-datalake` (placeholder name — confirm against the aws-org handoff
   notes and update `_terraform-run.yml`'s `Resolve target AWS account and role` step if it
   differs) trusting this repo's GitHub OIDC provider, with permissions to manage the resources in
   `terraform/modules/*`.
2. **Real Terraform state backend**: `terraform/envs/{dev,test}/backend.tf` still have `TODO-*`
   placeholder bucket/DynamoDB table names (see `docs/STATUS.md`) — `terraform init` will fail
   until those are real.
3. **GitHub Environments**: create `dev`, `test`, `dev-locked`, `test-locked` under repo
   Settings → Environments. Add required reviewers to the `*-locked` ones only — that's what makes
   `terraform-deploy-locked.yml` actually pause for approval; the plain ones should stay
   unprotected so `terraform-deploy.yml` runs immediately.
4. Populate each environment's `.tfvars` (`export_subnet_ids`, `export_security_group_ids`,
   `postgres_secret_arn`, `alarm_email`) — currently empty placeholders.

## Still TODO

- Plan-on-PR / apply-on-merge automation (the brief's actual Section 8 wording) — not built yet;
  today it's manual `workflow_dispatch` only.

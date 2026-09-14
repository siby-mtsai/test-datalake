# bootstrap

Creates the S3 bucket + DynamoDB table that `terraform/envs/{dev,test}/` use as their remote
state backend. Mirrors `mtsai-commuter-infra/bootstrap/` (same resources, same naming
convention), just with `project_name = "mtsai-datalake"`.

Has no backend of its own — apply it locally once per account (or via
`.github/workflows/bootstrap.yaml`) before `terraform init` will work in `terraform/envs/dev` or
`terraform/envs/test`. See [`../.github/workflows/README.md`](../.github/workflows/README.md) and
[`../docs/STATUS.md`](../docs/STATUS.md) for current status.

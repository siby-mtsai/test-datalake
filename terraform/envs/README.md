# terraform/envs

One directory per environment. Each holds the root module invocation and per-environment
`.tfvars` (account ID, region, tags) per brief Section 8 conventions — no hard-coded account IDs
outside a given env's own `.tfvars`.

- **`dev/`** — implemented. Wires up `lake-bucket`, `glue-catalog`, `athena-workgroup`, and
  `export-task` for the Dev account (`517293881120`, `miracletraffic-india-dev`).
- **`test/`, `preprod/`, `prod/`** — stubs. Not implemented yet: their account IDs aren't
  confirmed, and promotion follows feature → develop → main only after Dev is signed off
  (brief Section 7). Prod promotion is additionally a Section 12 checkpoint.

See [`../../docs/STATUS.md`](../../docs/STATUS.md) for current blockers (AWS access, state
bucket/DynamoDB lock table names) before running `terraform init`/`plan`/`apply` against `dev/`.

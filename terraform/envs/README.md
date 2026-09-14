# terraform/envs

One directory per environment. Each holds the root module invocation; account ID/region/tags are
plain variable defaults in that env's own `variables.tf` (no `.tfvars` files — matching the
convention already used in `mtsai-commuter-infra`), so no account ID is hard-coded outside a given
env's own directory. State backend (bucket/key/lock table) lives in each env's `versions.tf`, and
points at resources created once by [`../../bootstrap/`](../../bootstrap/).

- **`dev/`** — implemented. Wires up `lake-bucket`, `glue-catalog`, `athena-workgroup`, and
  `export-task` for the Dev account (`517293881120`, `miracletraffic-india-dev`).
- **`test/`** — implemented, same structure as `dev/`, for account `690293068614`. Not yet
  applied — per brief Section 7 the intended flow is Dev first (verify `terraform plan`/`apply`
  works and Phase 1's "done when" is met there), then promote to Test. This was scaffolded ahead
  of that for convenience; treat Dev as the one to actually stand up and verify first.
- **`preprod/`, `prod/`** — stubs. Not implemented yet: their account IDs aren't confirmed, and
  prod promotion is additionally a Section 12 checkpoint.

See [`../../docs/STATUS.md`](../../docs/STATUS.md) for current blockers (AWS access, running
`bootstrap/` once per account) before running `terraform init`/`plan`/`apply` against `dev/`.

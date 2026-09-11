# module: lake-bucket

Phase 1. Provisions the per-environment S3 bucket per brief Section 4/6: versioning on, SSE-KMS
with a customer-managed key per environment, block public access, lifecycle rules (Intelligent
Tiering then Glacier), access logging, and the `raw/`, `curated/`, `athena-results/`,
`export-manifests/` prefixes (Section 4.2).

Not yet implemented — blocked on AWS access (see [`../../../docs/STATUS.md`](../../../docs/STATUS.md)).

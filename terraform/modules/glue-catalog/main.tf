# One Glue database per zone (brief Section 4.1): raw and curated.

resource "aws_glue_catalog_database" "raw" {
  name = "${var.environment}_raw"

  description = "MTSAi data lake (${var.environment}) - raw zone: faithful copy of source rows, never modified except for erasure."
}

resource "aws_glue_catalog_database" "curated" {
  name = "${var.environment}_curated"

  description = "MTSAi data lake (${var.environment}) - curated zone: cleaned, deduplicated, typed tables for analytics."
}

# No explicit `tags` argument here - confirmed via `aws glue get-tags` that these still end up
# tagged (Project/Environment/Owner) through the AWS provider's `default_tags` block in each env's
# providers.tf, which applies to any resource whose schema supports tags even without an explicit
# `tags` argument on the resource itself. This matters beyond bookkeeping: the athena-workgroup
# module's GlueCatalogRead IAM statement conditions on aws:ResourceTag/Environment, so without
# this tag actually landing, no consumer role could read Glue metadata at all.

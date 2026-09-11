# One Glue database per zone (brief Section 4.1): raw and curated.

resource "aws_glue_catalog_database" "raw" {
  name = "${var.environment}_raw"

  description = "MTSAi data lake (${var.environment}) - raw zone: faithful copy of source rows, never modified except for erasure."
}

resource "aws_glue_catalog_database" "curated" {
  name = "${var.environment}_curated"

  description = "MTSAi data lake (${var.environment}) - curated zone: cleaned, deduplicated, typed tables for analytics."
}

# aws_glue_catalog_database has no native `tags` argument prior to resource tagging support
# landing across all Glue resources; tag the databases via the generic resourcegroupstaggingapi
# path instead if org policy requires it. Left untagged here deliberately - revisit if AWS Config
# rules flag it.

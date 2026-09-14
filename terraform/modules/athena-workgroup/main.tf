# One Athena workgroup + IAM role per consumer (brief Section 4, 6): analytics, forecasting, audit.
# Each is isolated to its own workgroup, its own query-scan limit, and read-only access to only
# the zones it needs (curated for all, raw additionally for audit).

resource "aws_athena_workgroup" "consumer" {
  for_each = var.consumers
  name     = "mtsai-datalake-${var.environment}-${each.key}"

  configuration {
    enforce_workgroup_configuration    = true
    bytes_scanned_cutoff_per_query     = each.value.bytes_scanned_cutoff_per_query
    publish_cloudwatch_metrics_enabled = true

    result_configuration {
      output_location = "s3://${var.bucket_name}/athena-results/${each.key}/"

      encryption_configuration {
        encryption_option = "SSE_KMS"
        kms_key_arn       = var.kms_key_arn
      }
    }
  }

  # "Workgroup" tag drives the aws_budgets_budget cost_filter below - AWS Budgets filters by
  # cost-allocation tag, not by the Athena workgroup name itself.
  tags = merge(var.tags, { Workgroup = each.key })
}

data "aws_iam_policy_document" "consumer_trust" {
  for_each = var.consumers

  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type = "AWS"
      identifiers = length(each.value.assumed_by_arns) > 0 ? each.value.assumed_by_arns : [
        "arn:aws:iam::${var.account_id}:root"
      ]
    }
  }
}

resource "aws_iam_role" "consumer" {
  for_each           = var.consumers
  name               = "mtsai-datalake-${var.environment}-${each.key}"
  assume_role_policy = data.aws_iam_policy_document.consumer_trust[each.key].json
  tags               = var.tags
}

data "aws_iam_policy_document" "consumer_access" {
  for_each = var.consumers

  statement {
    sid = "AthenaWorkgroupOnly"
    actions = [
      "athena:StartQueryExecution",
      "athena:StopQueryExecution",
      "athena:GetQueryExecution",
      "athena:GetQueryResults",
      "athena:GetWorkGroup",
      "athena:ListQueryExecutions",
    ]
    resources = [aws_athena_workgroup.consumer[each.key].arn]
  }

  # Glue's authorization model checks the WHOLE resource hierarchy for any action that touches a
  # table - reading a table requires an Allow on the catalog resource AND the database resource
  # AND the table resource, not just the most specific one. This statement covers the catalog
  # level (a fixed, un-taggable singleton, so it can't use the tag-based approach anyway); the
  # GlueCuratedRead/GlueRawRead statements below cover the database/table level. Both are required
  # together - found the hard way via a real AccessDeniedException naming the catalog ARN even
  # though the database/table ARNs were already correctly granted.
  statement {
    sid = "GlueCatalogRoot"
    actions = [
      "glue:GetDatabase",
      "glue:GetDatabases",
      "glue:GetTable",
      "glue:GetTables",
      "glue:GetPartition",
      "glue:GetPartitions",
    ]
    resources = ["arn:aws:glue:*:${var.account_id}:catalog"]
  }

  # Scoped by explicit Glue ARN (catalog/database/table hierarchy), not by resource tag: tables
  # are created by hand via Athena DDL/CTAS (brief Section 7, Phase 1 step 4), not by Terraform,
  # so they never get the Environment tag from a provider's default_tags - a tag-based condition
  # here would silently deny glue:GetTable on every real table (Athena surfaces that as a
  # confusing TABLE_NOT_FOUND, not a permission error, which is how this was actually found).
  # Scoping by database ARN also properly isolates raw-zone metadata by zone, which the previous
  # environment-wide tag condition never did.
  statement {
    sid = "GlueCuratedRead"
    actions = [
      "glue:GetDatabase",
      "glue:GetTable",
      "glue:GetTables",
      "glue:GetPartition",
      "glue:GetPartitions",
    ]
    resources = [
      "arn:aws:glue:*:${var.account_id}:database/${var.curated_database_name}",
      "arn:aws:glue:*:${var.account_id}:table/${var.curated_database_name}/*",
    ]
  }

  dynamic "statement" {
    for_each = each.value.raw_zone_read_access ? [1] : []
    content {
      sid = "GlueRawRead"
      actions = [
        "glue:GetDatabase",
        "glue:GetTable",
        "glue:GetTables",
        "glue:GetPartition",
        "glue:GetPartitions",
      ]
      resources = [
        "arn:aws:glue:*:${var.account_id}:database/${var.raw_database_name}",
        "arn:aws:glue:*:${var.account_id}:table/${var.raw_database_name}/*",
      ]
    }
  }

  # Athena calls s3:GetBucketLocation on the results bucket to verify it before running any
  # query at all - a bucket-level action with no "prefix" concept, so it can't be scoped by
  # s3:prefix like the ListBucket statements below. Missing this produces "Unable to
  # verify/create output bucket" on every query, for every consumer, regardless of which zone
  # they're actually querying.
  statement {
    sid       = "ResultsBucketLocation"
    actions   = ["s3:GetBucketLocation"]
    resources = ["arn:aws:s3:::${var.bucket_name}"]
  }

  # s3:ListBucket is a bucket-level action - granting it on the bare bucket ARN, even alongside a
  # prefix-scoped s3:GetObject, allows listing (enumerating keys under) the WHOLE bucket, not just
  # the granted prefix. It must be scoped separately via an s3:prefix condition, or every consumer
  # can enumerate every other zone's object keys regardless of their GetObject restrictions.
  statement {
    sid       = "CuratedZoneGet"
    actions   = ["s3:GetObject"]
    resources = ["arn:aws:s3:::${var.bucket_name}/curated/*"]
  }

  statement {
    sid       = "CuratedZoneList"
    actions   = ["s3:ListBucket"]
    resources = ["arn:aws:s3:::${var.bucket_name}"]
    condition {
      test     = "StringLike"
      variable = "s3:prefix"
      values   = ["curated", "curated/*"]
    }
  }

  dynamic "statement" {
    for_each = each.value.raw_zone_read_access ? [1] : []
    content {
      sid       = "RawZoneGet"
      actions   = ["s3:GetObject"]
      resources = ["arn:aws:s3:::${var.bucket_name}/raw/*"]
    }
  }

  dynamic "statement" {
    for_each = each.value.raw_zone_read_access ? [1] : []
    content {
      sid       = "RawZoneList"
      actions   = ["s3:ListBucket"]
      resources = ["arn:aws:s3:::${var.bucket_name}"]
      condition {
        test     = "StringLike"
        variable = "s3:prefix"
        values   = ["raw", "raw/*"]
      }
    }
  }

  statement {
    sid       = "AthenaResultsReadWrite"
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["arn:aws:s3:::${var.bucket_name}/athena-results/${each.key}/*"]
  }

  statement {
    sid       = "AthenaResultsList"
    actions   = ["s3:ListBucket"]
    resources = ["arn:aws:s3:::${var.bucket_name}"]
    condition {
      test     = "StringLike"
      variable = "s3:prefix"
      values   = ["athena-results/${each.key}", "athena-results/${each.key}/*"]
    }
  }

  statement {
    sid       = "ResultsDecrypt"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [var.kms_key_arn]
  }
}

resource "aws_iam_role_policy" "consumer" {
  for_each = var.consumers
  name     = "mtsai-datalake-${var.environment}-${each.key}-access"
  role     = aws_iam_role.consumer[each.key].id
  policy   = data.aws_iam_policy_document.consumer_access[each.key].json
}

# Monthly cost alarm per consumer (brief Section 4 observability, Section 6). Requires
# cost-allocation tags to be activated at the billing/org level - not yet confirmed (see module
# README) - so budgets are only created once notification emails are actually supplied.
resource "aws_budgets_budget" "consumer" {
  for_each     = length(var.budget_notification_emails) > 0 ? var.consumers : {}
  name         = "mtsai-datalake-${var.environment}-${each.key}"
  budget_type  = "COST"
  limit_amount = tostring(each.value.monthly_budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  cost_filter {
    name = "TagKeyValue"
    # AWS Budgets tag-value filter format is "user:<TagKey>$<TagValue>" - built with format() since
    # a literal "$" immediately before an interpolation can't be written directly in an HCL string.
    values = [format("user:Workgroup$%s", each.key)]
  }

  dynamic "notification" {
    for_each = var.budget_notification_emails
    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = 100
      threshold_type             = "PERCENTAGE"
      notification_type          = "ACTUAL"
      subscriber_email_addresses = [notification.value]
    }
  }
}

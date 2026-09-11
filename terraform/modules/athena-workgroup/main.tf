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

  statement {
    sid = "GlueCatalogRead"
    actions = [
      "glue:GetDatabase",
      "glue:GetDatabases",
      "glue:GetTable",
      "glue:GetTables",
      "glue:GetPartition",
      "glue:GetPartitions",
    ]
    resources = ["*"] # Glue catalog resources are account-scoped; narrowed via database name below.
    condition {
      test     = "StringEquals"
      variable = "aws:ResourceTag/Environment"
      values   = [var.environment]
    }
  }

  statement {
    sid       = "CuratedZoneRead"
    actions   = ["s3:GetObject", "s3:ListBucket"]
    resources = ["arn:aws:s3:::${var.bucket_name}/curated/*", "arn:aws:s3:::${var.bucket_name}"]
  }

  dynamic "statement" {
    for_each = each.value.raw_zone_read_access ? [1] : []
    content {
      sid       = "RawZoneRead"
      actions   = ["s3:GetObject", "s3:ListBucket"]
      resources = ["arn:aws:s3:::${var.bucket_name}/raw/*", "arn:aws:s3:::${var.bucket_name}"]
    }
  }

  statement {
    sid       = "AthenaResultsReadWrite"
    actions   = ["s3:GetObject", "s3:PutObject", "s3:ListBucket"]
    resources = ["arn:aws:s3:::${var.bucket_name}/athena-results/${each.key}/*", "arn:aws:s3:::${var.bucket_name}"]
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

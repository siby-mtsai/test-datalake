# S3 + KMS storage layer for the data lake (brief Section 4, 4.2, 6).

resource "aws_kms_key" "lake" {
  description             = "MTSAi data lake (${var.environment}) - S3 SSE-KMS"
  deletion_window_in_days = 30
  enable_key_rotation     = true
  tags                    = var.tags
}

resource "aws_kms_alias" "lake" {
  name          = "alias/mtsai-datalake-${var.environment}"
  target_key_id = aws_kms_key.lake.key_id
}

resource "aws_s3_bucket" "lake" {
  # brief Section 4.2: mtsai-datalake-{env}-{account-id}-{region}
  bucket = "mtsai-datalake-${var.environment}-${var.account_id}-${var.region}"
  tags   = var.tags
}

resource "aws_s3_bucket_versioning" "lake" {
  bucket = aws_s3_bucket.lake.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "lake" {
  bucket = aws_s3_bucket.lake.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.lake.arn
    }
    # Reduces per-request KMS cost (brief Section 10: "use bucket keys to reduce request volume").
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_public_access_block" "lake" {
  bucket                  = aws_s3_bucket.lake.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Access logs must go to a SEPARATE bucket, not the lake bucket itself: S3 server access logging
# does not support delivering logs to an SSE-KMS-encrypted destination (only SSE-S3), and the lake
# bucket's default encryption is SSE-KMS. Self-targeting silently delivers nothing - found by
# checking whether any log objects had actually landed after hours of real traffic; they hadn't.
resource "aws_s3_bucket" "lake_logs" {
  bucket = "mtsai-datalake-${var.environment}-${var.account_id}-${var.region}-logs"
  tags   = var.tags
}

resource "aws_s3_bucket_server_side_encryption_configuration" "lake_logs" {
  bucket = aws_s3_bucket.lake_logs.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256" # SSE-S3, not KMS - required for log delivery to work at all.
    }
  }
}

resource "aws_s3_bucket_public_access_block" "lake_logs" {
  bucket                  = aws_s3_bucket.lake_logs.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Since April 2023, new S3 buckets default to Object Ownership "Bucket owner enforced" (ACLs
# disabled) - that's the case here, confirmed via `aws s3api get-bucket-ownership-controls`. With
# ACLs disabled, the classic "grant the S3 Log Delivery group access via ACL" mechanism can't
# apply at all, so server access logging silently never delivers anything unless the target
# bucket has an explicit policy granting the logging service principal permission instead. Found
# by checking for both a policy and any actual log objects after real traffic - there was
# neither.
data "aws_iam_policy_document" "lake_logs_delivery" {
  statement {
    sid     = "S3ServerAccessLogsPolicy"
    actions = ["s3:PutObject"]
    principals {
      type        = "Service"
      identifiers = ["logging.s3.amazonaws.com"]
    }
    resources = ["${aws_s3_bucket.lake_logs.arn}/access-logs/*"]
    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = [aws_s3_bucket.lake.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [var.account_id]
    }
  }
}

resource "aws_s3_bucket_policy" "lake_logs_delivery" {
  bucket = aws_s3_bucket.lake_logs.id
  policy = data.aws_iam_policy_document.lake_logs_delivery.json
}

resource "aws_s3_bucket_lifecycle_configuration" "lake_logs" {
  bucket = aws_s3_bucket.lake_logs.id

  rule {
    id     = "expire-old-logs"
    status = "Enabled"

    filter {}

    expiration {
      days = 90
    }
  }
}

resource "aws_s3_bucket_logging" "lake" {
  bucket        = aws_s3_bucket.lake.id
  target_bucket = aws_s3_bucket.lake_logs.id
  target_prefix = "access-logs/"
}

resource "aws_s3_bucket_lifecycle_configuration" "lake" {
  bucket = aws_s3_bucket.lake.id

  rule {
    id     = "raw-tiering"
    status = "Enabled"

    filter {
      prefix = "raw/"
    }

    transition {
      days          = var.intelligent_tiering_days
      storage_class = "INTELLIGENT_TIERING"
    }

    transition {
      days          = var.glacier_days
      storage_class = "GLACIER_IR"
    }
  }

  rule {
    id     = "athena-results-expiry"
    status = "Enabled"

    filter {
      prefix = "athena-results/"
    }

    expiration {
      days = var.athena_results_expiration_days
    }
  }
}

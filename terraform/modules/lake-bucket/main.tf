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

resource "aws_s3_bucket_logging" "lake" {
  bucket        = aws_s3_bucket.lake.id
  target_bucket = aws_s3_bucket.lake.id
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

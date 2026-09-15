output "bucket_name" {
  value = aws_s3_bucket.lake.id
}

output "bucket_arn" {
  value = aws_s3_bucket.lake.arn
}

output "kms_key_arn" {
  value = aws_kms_key.lake.arn
}

output "kms_key_id" {
  value = aws_kms_key.lake.key_id
}

output "logs_bucket_name" {
  value = aws_s3_bucket.lake_logs.id
}

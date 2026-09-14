variable "environment" {
  description = "Environment name (dev, test, preprod, prod)."
  type        = string
}

variable "account_id" {
  description = "AWS account ID, used to scope Glue catalog/database/table ARNs precisely."
  type        = string
}

variable "bucket_arn" {
  description = "Lake bucket ARN (from the lake-bucket module)."
  type        = string
}

variable "bucket_name" {
  description = "Lake bucket name (from the lake-bucket module)."
  type        = string
}

variable "kms_key_arn" {
  description = "KMS key ARN used to encrypt objects the export task writes."
  type        = string
}

variable "raw_database_name" {
  description = "Glue database name for the raw zone the export task writes into."
  type        = string
}

variable "postgres_secret_arn" {
  description = "Secrets Manager ARN for the dedicated low-privilege Postgres read role (brief Section 6). Empty until Phase 0 confirms the read replica / role - the IAM policy grants no access when empty."
  type        = string
  default     = ""
}

variable "container_image" {
  description = "Export job container image. Placeholder until the Phase 2 Go export job (mtsai-datalake-export) is built and pushed."
  type        = string
  default     = "public.ecr.aws/docker/library/hello-world:latest"
}

variable "schedule_expression" {
  description = "EventBridge Scheduler cron/rate expression for the nightly export run."
  type        = string
  default     = "cron(0 20 * * ? *)" # 20:00 UTC == 01:30 IST, inside the assumed low-traffic window
}

variable "cpu" {
  description = "Fargate task CPU units."
  type        = string
  default     = "512"
}

variable "memory" {
  description = "Fargate task memory (MiB)."
  type        = string
  default     = "1024"
}

variable "subnet_ids" {
  description = "Private subnet IDs the Fargate task runs in. Must be populated before first apply - left empty as a placeholder pending VPC/network confirmation."
  type        = list(string)
  default     = []
}

variable "security_group_ids" {
  description = "Security group IDs for the Fargate task's network interface. Must allow egress to RDS and AWS service endpoints."
  type        = list(string)
  default     = []
}

variable "alarm_email" {
  description = "Email subscribed to the export job failure SNS topic. Empty skips the subscription (topic and alarm are still created)."
  type        = string
  default     = ""
}

variable "tags" {
  description = "Tags applied to every resource in this module."
  type        = map(string)
}

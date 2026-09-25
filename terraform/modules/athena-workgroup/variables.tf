variable "environment" {
  description = "Environment name (dev, test, preprod, prod)."
  type        = string
}

variable "bucket_name" {
  description = "Lake bucket name (from the lake-bucket module) holding athena-results/."
  type        = string
}

variable "kms_key_arn" {
  description = "KMS key ARN used to encrypt Athena query results."
  type        = string
}

variable "curated_database_name" {
  description = "Glue database name for the curated zone (read access for all consumers)."
  type        = string
}

variable "raw_database_name" {
  description = "Glue database name for the raw zone (read access for the audit consumer only)."
  type        = string
}

variable "consumers" {
  description = "One entry per consumer workgroup (brief Section 6: analytics, forecasting, audit)."
  type = map(object({
    bytes_scanned_cutoff_per_query = number
    monthly_budget_usd             = number
    raw_zone_read_access           = bool
    # ARNs allowed to assume this consumer's role. Left empty (defaults to the account root) until
    # the actual mtsai-analytics / forecasting / audit service principals are confirmed - TODO.
    assumed_by_arns = optional(list(string), [])
    # Lets this consumer launch the existing erasure task (and read its manifests), for the
    # mtsai-analytics dashboard's Erase feature. The role still gets no direct delete/write access
    # to lake data - the erasure task does the deleting under its own task role.
    erasure_access = optional(bool, false)
  }))
  default = {
    analytics = {
      bytes_scanned_cutoff_per_query = 5368709120 # 5 GB
      monthly_budget_usd             = 50
      raw_zone_read_access           = false
    }
    forecasting = {
      bytes_scanned_cutoff_per_query = 5368709120
      monthly_budget_usd             = 50
      raw_zone_read_access           = false
    }
    audit = {
      bytes_scanned_cutoff_per_query = 10737418240 # 10 GB
      monthly_budget_usd             = 25
      raw_zone_read_access           = true
    }
  }
}

variable "account_id" {
  description = "AWS account ID, used as the fallback trust-policy principal until per-consumer principals are confirmed."
  type        = string
}

variable "budget_notification_emails" {
  description = "Email addresses notified when a consumer's monthly Athena budget is exceeded. Required for aws_budgets_budget notifications; leave empty to skip creating budgets until confirmed (see module README re: cost-allocation tags)."
  type        = list(string)
  default     = []
}

variable "erasure_task_definition_arn" {
  description = "ARN of the erasure ECS task definition (any revision; the policy grants the whole family). Empty disables the erasure grants."
  type        = string
  default     = ""
}

variable "erasure_cluster_arn" {
  description = "ARN of the ECS cluster the erasure task runs on. Empty disables the erasure grants."
  type        = string
  default     = ""
}

variable "erasure_pass_role_arns" {
  description = "IAM roles the erasure task definition references (task role + execution role), which ecs:RunTask requires iam:PassRole on. Empty disables the erasure grants."
  type        = list(string)
  default     = []
}

variable "tags" {
  description = "Tags applied to every resource in this module."
  type        = map(string)
}

variable "environment" {
  description = "Environment name (dev, test, preprod, prod)."
  type        = string
}

variable "account_id" {
  description = "AWS account ID this bucket is provisioned in."
  type        = string
}

variable "region" {
  description = "AWS region for the bucket name suffix."
  type        = string
  default     = "ap-south-1"
}

variable "intelligent_tiering_days" {
  description = "Days after object creation before transition to S3 Intelligent-Tiering."
  type        = number
  default     = 30
}

variable "glacier_days" {
  description = "Days after object creation before raw/ transitions to Glacier Instant Retrieval."
  type        = number
  default     = 365
}

variable "athena_results_expiration_days" {
  description = "Days before objects under athena-results/ expire."
  type        = number
  default     = 7
}

variable "tags" {
  description = "Tags applied to every resource in this module."
  type        = map(string)
}

variable "environment" {
  description = "Environment name."
  type        = string
  default     = "dev"
}

variable "account_id" {
  description = "AWS account ID (miracletraffic-india-dev)."
  type        = string
  default     = "517293881120"
}

variable "region" {
  description = "AWS region."
  type        = string
  default     = "ap-south-1"
}

variable "owner" {
  description = "Owner tag - the implementer accountable for this environment's resources."
  type        = string
  default     = "Fizza"
}

variable "postgres_secret_arn" {
  description = "Secrets Manager ARN for the dedicated low-privilege Postgres read role. Empty until Phase 0 confirms the read replica / role."
  type        = string
  default     = ""
}

variable "export_subnet_ids" {
  description = "Private subnet IDs for the export Fargate task. Must be populated before first apply."
  type        = list(string)
  default     = []
}

variable "export_security_group_ids" {
  description = "Security group IDs for the export Fargate task."
  type        = list(string)
  default     = []
}

variable "alarm_email" {
  description = "Email subscribed to export job failure alerts and budget notifications."
  type        = string
  default     = ""
}

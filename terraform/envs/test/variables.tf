variable "environment" {
  description = "Environment name."
  type        = string
  default     = "test"
}

variable "account_id" {
  description = "AWS account ID for the Test environment."
  type        = string
  default     = "690293068614"
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

variable "mtsai_api_sim_vpc_id" {
  description = "VPC for the mtsai-api-sim stand-in database. Defaults to this account's default VPC (confirmed via `aws ec2 describe-vpcs` - no dedicated VPC exists yet)."
  type        = string
  default     = "vpc-00f4f72bf2315c98e"
}

variable "mtsai_api_sim_subnet_ids" {
  description = "Subnets for the mtsai-api-sim DB subnet group. Defaults to the default VPC's three public subnets (no private subnets/NAT exist yet)."
  type        = list(string)
  default = [
    "subnet-0266f942977861ddd",
    "subnet-05639de4c9e0732b7",
    "subnet-08064e40f903eceab",
  ]
}

variable "mtsai_api_sim_client_cidr" {
  description = "Single CIDR allowed to reach the mtsai-api-sim database - no bastion/VPN exists in this VPC, so this is the access control instead of network isolation. Update if the client's public IP changes."
  type        = string
  default     = "103.189.214.130/32"
}

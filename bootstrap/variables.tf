variable "aws_region" {
  description = "AWS region for the Terraform state backend resources"
  type        = string
  default     = "ap-south-1"
}

variable "project_name" {
  description = "Project name used to prefix backend resource names"
  type        = string
  default     = "mtsai-datalake"
}

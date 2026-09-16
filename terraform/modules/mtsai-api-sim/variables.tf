variable "environment" {
  description = "Environment name (dev, test)."
  type        = string
}

variable "vpc_id" {
  description = "VPC to launch the instance in."
  type        = string
}

variable "subnet_ids" {
  description = "Subnets for the DB subnet group."
  type        = list(string)
}

variable "client_cidr" {
  description = "Single CIDR (e.g. a /32) allowed to reach Postgres. No bastion/VPN exists in this VPC, so this is the access control instead of network isolation - keep it narrow."
  type        = string
}

variable "instance_class" {
  description = "RDS instance class. db.t4g.micro (Graviton) hit InsufficientDBInstanceCapacity in ap-south-1 at creation time - db.t3.micro (x86) used instead, same price tier."
  type        = string
  default     = "db.t3.micro"
}

variable "allocated_storage_gb" {
  description = "Allocated storage in GB."
  type        = number
  default     = 20
}

variable "engine_version" {
  description = "Postgres engine version."
  type        = string
  default     = "17.11"
}

variable "db_name" {
  description = "Initial database name."
  type        = string
  default     = "mtsai_api_sim"
}

variable "master_username" {
  description = "Master username."
  type        = string
  default     = "mtsai_admin"
}

variable "tags" {
  description = "Tags applied to every resource in this module."
  type        = map(string)
}

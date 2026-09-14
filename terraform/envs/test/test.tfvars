# Non-sensitive defaults for the Test environment. Most values already default correctly in
# variables.tf; override here once confirmed:
# - postgres_secret_arn: set once Phase 0 confirms the read-replica / low-privilege role.
# - export_subnet_ids / export_security_group_ids: set once the VPC/network for Test is confirmed.
# - alarm_email: set to receive export-failure and budget alerts.

environment = "test"
account_id  = "690293068614"
region      = "ap-south-1"
owner       = "Fizza"

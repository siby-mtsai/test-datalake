variable "environment" {
  description = "Environment name."
  type        = string
}

variable "region" {
  description = "AWS region."
  type        = string
}

variable "consumer_workgroup_names" {
  description = "Athena workgroup names to chart bytes-scanned for (the analytics/forecasting/audit consumer workgroups)."
  type        = map(string)
}

variable "pipeline_workgroup_name" {
  description = "The export-task module's own Athena workgroup name (export/curate/compact/erasure's own queries)."
  type        = string
}

variable "export_log_group_name" {
  description = "CloudWatch Logs group shared by export/curate/compact/erasure - used for the Fargate-minutes widget, parsed from each run's own duration=X.XXs log line."
  type        = string
}

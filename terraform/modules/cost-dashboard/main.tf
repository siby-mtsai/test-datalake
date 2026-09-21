# Cost dashboard (brief Section 7 Phase 3 step 4: "S3 storage by prefix, Athena bytes scanned by
# workgroup, Fargate minutes"). Budget alarms themselves are the already-existing
# aws_budgets_budget resources in terraform/modules/athena-workgroup/main.tf, activated by setting
# var.alarm_email at the env level - nothing to add here for those.

locals {
  # athena-results/export, not the whole athena-results/ zone - matches cmd/compact's own
  # ListBucket grant exactly (scoped to its own subfolder, not the other consumer workgroups').
  storage_prefixes = ["raw", "curated", "athena-results/export", "export-manifests"]

  # One [Namespace, MetricName, DimensionName, DimensionValue] tuple per prefix - matches
  # cmd/compact's weekly PutMetricData calls exactly (metric name, dimension name/values).
  storage_metrics = [
    for p in local.storage_prefixes :
    ["MTSAiDataLake/Storage", "PrefixBytes", "Prefix", p]
  ]

  # Athena's native ProcessedBytes metric, dimensioned by WorkGroup - no custom code needed, this
  # already exists per workgroup. Consumer workgroups (analytics/forecasting/audit) plus the
  # pipeline's own.
  athena_workgroups = merge(var.consumer_workgroup_names, { pipeline = var.pipeline_workgroup_name })
  athena_metrics = [
    for name in values(local.athena_workgroups) :
    ["AWS/Athena", "ProcessedBytes", "WorkGroup", name]
  ]
}

resource "aws_cloudwatch_dashboard" "cost" {
  dashboard_name = "mtsai-datalake-${var.environment}-cost"

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          title   = "S3 storage by prefix"
          region  = var.region
          view    = "timeSeries"
          stacked = true
          period  = 86400
          stat    = "Maximum"
          metrics = local.storage_metrics
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          title   = "Athena bytes scanned by workgroup"
          region  = var.region
          view    = "timeSeries"
          stacked = false
          period  = 86400
          stat    = "Sum"
          metrics = local.athena_metrics
        }
      },
      {
        type   = "log"
        x      = 0
        y      = 6
        width  = 24
        height = 6
        properties = {
          title  = "Fargate run duration by day (export/curate/compact/erasure)"
          region = var.region
          view   = "table"
          query  = <<-QUERY
            SOURCE '${var.export_log_group_name}'
            | fields @message
            | parse @message /duration=(?<duration_seconds>[0-9.]+)s/
            | filter ispresent(duration_seconds)
            | stats sum(duration_seconds) as total_seconds, count(*) as run_count by bin(1d)
          QUERY
        }
      }
    ]
  })
}

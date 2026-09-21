output "bucket_name" {
  value = module.lake_bucket.bucket_name
}

output "raw_database_name" {
  value = module.glue_catalog.raw_database_name
}

output "curated_database_name" {
  value = module.glue_catalog.curated_database_name
}

output "consumer_role_arns" {
  value = module.athena_workgroup.consumer_role_arns
}

output "export_cluster_arn" {
  value = module.export_task.cluster_arn
}

output "mtsai_api_sim_endpoint" {
  value = module.mtsai_api_sim.endpoint
}

output "mtsai_api_sim_secret_arn" {
  value = module.mtsai_api_sim.secret_arn
}

output "export_ecr_repository_url" {
  value = module.export_task.ecr_repository_url
}

output "curate_task_definition_arn" {
  value = module.export_task.curate_task_definition_arn
}

output "compact_task_definition_arn" {
  value = module.export_task.compact_task_definition_arn
}

output "erasure_task_definition_arn" {
  value = module.export_task.erasure_task_definition_arn
}

output "cost_dashboard_url" {
  value = module.cost_dashboard.dashboard_url
}

output "export_task_security_group_id" {
  value = module.export_task.export_task_security_group_id
}

output "export_schedule_enabled" {
  value = module.export_task.export_schedule_enabled
}

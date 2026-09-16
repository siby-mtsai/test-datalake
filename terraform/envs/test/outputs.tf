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

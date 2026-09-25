locals {
  common_tags = {
    Project     = "datalake"
    Environment = var.environment
    Owner       = var.owner
  }
}

module "lake_bucket" {
  source = "../../modules/lake-bucket"

  environment = var.environment
  account_id  = var.account_id
  region      = var.region
  tags        = local.common_tags
}

module "glue_catalog" {
  source = "../../modules/glue-catalog"

  environment = var.environment
  tags        = local.common_tags
}

module "athena_workgroup" {
  source = "../../modules/athena-workgroup"

  environment                = var.environment
  account_id                 = var.account_id
  bucket_name                = module.lake_bucket.bucket_name
  kms_key_arn                = module.lake_bucket.kms_key_arn
  curated_database_name      = module.glue_catalog.curated_database_name
  raw_database_name          = module.glue_catalog.raw_database_name
  budget_notification_emails = var.alarm_email != "" ? [var.alarm_email] : []
  tags                       = local.common_tags

  # Lets each consumer launch the existing erasure task for the mtsai-analytics dashboard's Erase
  # feature (docs/prompts/001-consumer-erasure-access.md). Only export_task -> athena_workgroup
  # references, never the reverse, so there's no dependency cycle.
  erasure_task_definition_arn = module.export_task.erasure_task_definition_arn
  erasure_cluster_arn         = module.export_task.cluster_arn
  erasure_pass_role_arns      = [module.export_task.task_role_arn, module.export_task.execution_role_arn]

  # The module's defaults, copied exactly, with erasure_access added.
  consumers = {
    analytics = {
      bytes_scanned_cutoff_per_query = 5368709120 # 5 GB
      monthly_budget_usd             = 50
      raw_zone_read_access           = false
      erasure_access                 = true
    }
    forecasting = {
      bytes_scanned_cutoff_per_query = 5368709120
      monthly_budget_usd             = 50
      raw_zone_read_access           = false
      erasure_access                 = true
    }
    audit = {
      bytes_scanned_cutoff_per_query = 10737418240 # 10 GB
      monthly_budget_usd             = 25
      raw_zone_read_access           = true
      erasure_access                 = true
    }
  }
}

module "mtsai_api_sim" {
  source = "../../modules/mtsai-api-sim"

  environment = var.environment
  vpc_id      = var.mtsai_api_sim_vpc_id
  subnet_ids  = var.mtsai_api_sim_subnet_ids
  client_cidr = var.mtsai_api_sim_client_cidr
  tags        = local.common_tags
}

module "export_task" {
  source = "../../modules/export-task"

  environment           = var.environment
  account_id            = var.account_id
  bucket_arn            = module.lake_bucket.bucket_arn
  bucket_name           = module.lake_bucket.bucket_name
  kms_key_arn           = module.lake_bucket.kms_key_arn
  raw_database_name     = module.glue_catalog.raw_database_name
  curated_database_name = module.glue_catalog.curated_database_name
  postgres_secret_arn   = module.mtsai_api_sim.secret_arn
  container_image       = var.export_container_image
  vpc_id                = var.mtsai_api_sim_vpc_id
  subnet_ids            = var.mtsai_api_sim_subnet_ids
  db_security_group_id  = module.mtsai_api_sim.security_group_id
  alarm_email           = var.alarm_email
  alarm_phone_number    = var.alarm_phone_number
  tags                  = local.common_tags
}

module "cost_dashboard" {
  source = "../../modules/cost-dashboard"

  environment              = var.environment
  region                   = var.region
  consumer_workgroup_names = module.athena_workgroup.workgroup_names
  pipeline_workgroup_name  = module.export_task.pipeline_workgroup_name
  export_log_group_name    = module.export_task.log_group_name
}

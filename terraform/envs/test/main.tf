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
}

module "export_task" {
  source = "../../modules/export-task"

  environment         = var.environment
  account_id          = var.account_id
  bucket_arn          = module.lake_bucket.bucket_arn
  bucket_name         = module.lake_bucket.bucket_name
  kms_key_arn         = module.lake_bucket.kms_key_arn
  raw_database_name   = module.glue_catalog.raw_database_name
  postgres_secret_arn = var.postgres_secret_arn
  subnet_ids          = var.export_subnet_ids
  security_group_ids  = var.export_security_group_ids
  alarm_email         = var.alarm_email
  tags                = local.common_tags
}

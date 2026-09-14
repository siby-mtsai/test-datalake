# Terraform state per environment in the existing state bucket with DynamoDB locking
# (brief Section 8 convention). Bucket/table names below are placeholders - TODO: confirm the
# actual state bucket and lock table names against the aws-org handoff notes before first
# `terraform init`. These are also unconfirmed for dev (see envs/dev/backend.tf) - likely the same
# shared state bucket/table across environments, distinguished by `key`, but confirm before use.
terraform {
  backend "s3" {
    bucket         = "TODO-mtsai-terraform-state-bucket"
    key            = "datalake/test/terraform.tfstate"
    region         = "ap-south-1"
    dynamodb_table = "TODO-mtsai-terraform-locks"
    encrypt        = true
  }
}

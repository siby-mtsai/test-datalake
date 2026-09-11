# Terraform state per environment in the existing state bucket with DynamoDB locking
# (brief Section 8 convention). Values below are placeholders - TODO: confirm the actual state
# bucket and lock table names against the aws-org handoff notes before first `terraform init`.
terraform {
  backend "s3" {
    bucket         = "TODO-mtsai-terraform-state-bucket"
    key            = "datalake/dev/terraform.tfstate"
    region         = "ap-south-1"
    dynamodb_table = "TODO-mtsai-terraform-locks"
    encrypt        = true
  }
}

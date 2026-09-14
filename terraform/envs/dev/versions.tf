terraform {
  required_version = ">= 1.15"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # State bucket/lock table names follow the same convention as mtsai-commuter-infra:
  # "<project_name>-tfstate-<account_id>" / "<project_name>-tflock", created by ../../../bootstrap/
  # applied once against this account. Run bootstrap first if these don't exist yet.
  backend "s3" {
    bucket         = "mtsai-datalake-tfstate-517293881120"
    key            = "dev/terraform.tfstate"
    region         = "ap-south-1"
    dynamodb_table = "mtsai-datalake-tflock"
    encrypt        = true
  }
}

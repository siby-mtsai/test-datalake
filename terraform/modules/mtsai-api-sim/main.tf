# Test-only synthetic stand-in for the real mtsai-api Postgres database (brief Section 4,
# "Source" row) - there is no copy of the real database anywhere reachable from this project, so
# this exists purely to unblock Phase 0 discovery and give Phase 2's export job something real
# (if synthetic) to eventually point at. Not a claim that it matches the real mtsai-api schema -
# see docs/STATUS.md and docs/classification-table.md for that caveat.

# No inline ingress/egress blocks here deliberately: this security group needs rules added from
# outside this module too (the scheduled export task's security group gets an ingress rule added
# by terraform/modules/export-task). Mixing inline blocks with separate rule resources on the same
# security group is a well-documented Terraform footgun (the inline-block resource treats itself
# as authoritative and fights any externally-added rule) - using standalone rule resources for
# every rule, from the start, avoids that entirely.
resource "aws_security_group" "db" {
  name        = "mtsai-api-sim-${var.environment}"
  description = "Postgres access for the mtsai-api-sim instance - locked to one client CIDR, no bastion exists in this VPC"
  vpc_id      = var.vpc_id
  tags        = var.tags
}

resource "aws_vpc_security_group_ingress_rule" "client_cidr" {
  security_group_id = aws_security_group.db.id
  description       = "Postgres from the confirmed client IP only"
  from_port         = 5432
  to_port           = 5432
  ip_protocol       = "tcp"
  cidr_ipv4         = var.client_cidr
  tags              = var.tags
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.db.id
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
  tags              = var.tags
}

resource "aws_db_subnet_group" "this" {
  name       = "mtsai-api-sim-${var.environment}"
  subnet_ids = var.subnet_ids
  tags       = var.tags
}

# shared_preload_libraries can only be set via a parameter group, and takes effect at instance
# creation here (not a later reboot) - required for Phase 0 step 2 (pg_stat_statements).
resource "aws_db_parameter_group" "this" {
  name   = "mtsai-api-sim-${var.environment}"
  family = "postgres17"

  parameter {
    name         = "shared_preload_libraries"
    value        = "pg_stat_statements"
    apply_method = "pending-reboot" # only takes effect at launch when set before first boot
  }

  tags = var.tags
}

resource "random_password" "master" {
  length  = 24
  special = false # keep it simple to embed in a connection string / JSON secret without escaping
}

resource "aws_db_instance" "this" {
  identifier     = "mtsai-api-sim-${var.environment}"
  engine         = "postgres"
  engine_version = var.engine_version
  instance_class = var.instance_class

  allocated_storage = var.allocated_storage_gb
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = var.db_name
  username = var.master_username
  password = random_password.master.result
  port     = 5432

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]
  parameter_group_name   = aws_db_parameter_group.this.name
  publicly_accessible    = true

  multi_az                = false
  backup_retention_period = 1
  skip_final_snapshot     = true
  deletion_protection     = false
  apply_immediately       = true

  tags = var.tags
}

# Stored in the exact JSON shape export/internal/config/config.go already parses
# (host/port/dbname/username/password) - so this secret's ARN could be wired straight into
# export-task's postgres_secret_arn later if Phase 2 should read from this instead of local Docker.
resource "aws_secretsmanager_secret" "credentials" {
  name                    = "mtsai-api-sim-${var.environment}-credentials"
  recovery_window_in_days = 0 # disposable synthetic-data instance, no need to protect against deletion
  tags                    = var.tags
}

resource "aws_secretsmanager_secret_version" "credentials" {
  secret_id = aws_secretsmanager_secret.credentials.id
  secret_string = jsonencode({
    host     = aws_db_instance.this.address
    port     = aws_db_instance.this.port
    dbname   = var.db_name
    username = var.master_username
    password = random_password.master.result
  })
}

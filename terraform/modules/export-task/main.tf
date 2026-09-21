# Scheduled ECS Fargate export job + its IAM, schedule, and failure alerting
# (brief Section 4 "Export" row, Section 6, Section 7 Phase 1 step 2 / Phase 2).

resource "aws_ecr_repository" "export" {
  name                 = "mtsai-datalake-${var.environment}-export"
  image_tag_mutability = "MUTABLE" # Test env, not a versioned release process yet

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = var.tags
}

resource "aws_ecr_lifecycle_policy" "export" {
  repository = aws_ecr_repository.export.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep only the last 5 images - Test env, no versioned release process yet"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 5
      }
      action = { type = "expire" }
    }]
  })
}

resource "aws_ecs_cluster" "export" {
  name = "mtsai-datalake-${var.environment}"
  tags = var.tags
}

resource "aws_cloudwatch_log_group" "export" {
  name              = "/mtsai-datalake/${var.environment}/export"
  retention_in_days = 30
  tags              = var.tags
}

# --- Task execution role: pulls the image, writes logs, reads the DB secret into the container ---

data "aws_iam_policy_document" "execution_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "execution" {
  name               = "mtsai-datalake-${var.environment}-export-execution"
  assume_role_policy = data.aws_iam_policy_document.execution_trust.json
  tags               = var.tags
}

resource "aws_iam_role_policy_attachment" "execution_managed" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

data "aws_iam_policy_document" "execution_secret" {
  count = var.postgres_secret_arn != "" ? 1 : 0

  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [var.postgres_secret_arn]
  }
}

resource "aws_iam_role_policy" "execution_secret" {
  count  = var.postgres_secret_arn != "" ? 1 : 0
  name   = "read-postgres-secret"
  role   = aws_iam_role.execution.id
  policy = data.aws_iam_policy_document.execution_secret[0].json
}

# --- Task role: the export job's own AWS permissions at runtime ---
# No Postgres write access - SELECT-only DB access is granted on the Postgres side via the secret
# above, not here (brief Section 6: "no write access to Postgres").

data "aws_iam_policy_document" "task_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "task" {
  name               = "mtsai-datalake-${var.environment}-export-task"
  assume_role_policy = data.aws_iam_policy_document.task_trust.json
  tags               = var.tags
}

data "aws_iam_policy_document" "task_access" {
  # s3:ListBucket is a bucket-level action - granting it on the bare bucket ARN (even alongside
  # prefix-scoped s3:PutObject/GetObject) allows listing the WHOLE bucket, not just raw/ and
  # export-manifests/. Scope it separately via an s3:prefix condition (see the same fix applied
  # to terraform/modules/athena-workgroup/main.tf, found via a real isolation test).
  statement {
    sid       = "WriteRawAndManifests"
    actions   = ["s3:PutObject", "s3:GetObject"]
    resources = ["${var.bucket_arn}/raw/*", "${var.bucket_arn}/export-manifests/*"]
  }

  statement {
    sid       = "ListRawAndManifests"
    actions   = ["s3:ListBucket"]
    resources = [var.bucket_arn]
    condition {
      test     = "StringLike"
      variable = "s3:prefix"
      values   = ["raw", "raw/*", "export-manifests", "export-manifests/*"]
    }
  }

  statement {
    sid       = "EncryptWrites"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [var.kms_key_arn]
  }

  # No dedicated Athena workgroup exists for this role (see CommitToIcebergQuery below), so every
  # query must pass its own ResultConfiguration.OutputLocation explicitly - which needs its own
  # write access, separate from raw/ and export-manifests/.
  statement {
    sid       = "AthenaResultsReadWrite"
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${var.bucket_arn}/athena-results/export/*"]
  }

  statement {
    sid       = "AthenaResultsList"
    actions   = ["s3:ListBucket"]
    resources = [var.bucket_arn]
    condition {
      test     = "StringLike"
      variable = "s3:prefix"
      values   = ["athena-results/export", "athena-results/export/*"]
    }
  }

  statement {
    sid       = "CommitToIcebergQuery"
    actions   = ["athena:StartQueryExecution", "athena:GetQueryExecution", "athena:GetQueryResults"]
    resources = ["*"] # No dedicated workgroup for the export task yet; revisit once Phase 2 gives it one.
  }

  # Same trap as terraform/modules/athena-workgroup/main.tf's GlueCuratedRead/GlueCatalogRoot:
  # tag-based conditions don't work for Glue tables (created via CTAS/INSERT, not Terraform, so
  # never carry a tag) or the catalog root (a fixed, un-taggable singleton) - scope by explicit
  # Glue ARN instead.
  # Glue's authorization model checks the WHOLE resource hierarchy for any action that touches a
  # table - it requires an Allow on the catalog resource AND the database/table resource, not just
  # the most specific one (found via a real AccessDeniedException naming the catalog ARN even
  # though the database/table ARNs were already granted - see the same fix in
  # athena-workgroup/main.tf for the read-side discovery). Covers both read and write actions since
  # this role also creates/updates tables.
  statement {
    sid = "GlueCatalogRoot"
    actions = [
      "glue:GetDatabase",
      "glue:GetDatabases",
      "glue:GetTable",
      "glue:GetTables",
      "glue:GetPartitions",
      "glue:CreateTable",
      "glue:UpdateTable",
      "glue:DeleteTable", # needed to drop the throwaway staging table each run creates and cleans up
      "glue:BatchCreatePartition",
    ]
    resources = ["arn:aws:glue:*:${var.account_id}:catalog"]
  }

  statement {
    sid = "CommitToIceberg"
    actions = [
      "glue:GetDatabase",
      "glue:GetTable",
      "glue:GetTables",
      "glue:GetPartitions",
      "glue:CreateTable",
      "glue:UpdateTable",
      "glue:DeleteTable",
      "glue:BatchCreatePartition",
    ]
    resources = [
      "arn:aws:glue:*:${var.account_id}:database/${var.raw_database_name}",
      "arn:aws:glue:*:${var.account_id}:table/${var.raw_database_name}/*",
    ]
  }
}

resource "aws_iam_role_policy" "task_access" {
  name   = "mtsai-datalake-${var.environment}-export-task-access"
  role   = aws_iam_role.task.id
  policy = data.aws_iam_policy_document.task_access.json
}

# --- Task definition (Fargate, ARM64) ---

resource "aws_ecs_task_definition" "export" {
  family                   = "mtsai-datalake-${var.environment}-export"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.cpu
  memory                   = var.memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn

  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }

  container_definitions = jsonencode([
    {
      name      = "export"
      image     = var.container_image
      essential = true
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.export.name
          "awslogs-region"        = data.aws_region.current.name
          "awslogs-stream-prefix" = "export"
        }
      }
      secrets = var.postgres_secret_arn != "" ? [
        { name = "POSTGRES_CREDENTIALS", valueFrom = var.postgres_secret_arn }
      ] : []
      environment = [
        { name = "MTSAI_DATALAKE_ENV", value = var.environment },
        { name = "MTSAI_DATALAKE_BUCKET", value = var.bucket_name },
        { name = "MTSAI_DATALAKE_RAW_DATABASE", value = var.raw_database_name },
      ]
    }
  ])

  tags = var.tags
}

data "aws_region" "current" {}

# --- Nightly schedule (EventBridge Scheduler -> ecs:RunTask) ---
# Fargate tasks need a VPC network config (subnets + a security group) to run at all. The schedule
# itself - and the IAM role that only exists to run it - are only created once vpc_id/subnet_ids
# are actually supplied, same as before this module owned its own security group.

locals {
  export_schedule_enabled = var.vpc_id != "" && length(var.subnet_ids) > 0
}

# No inbound needed - this task only makes outbound calls (to the database and to AWS service
# endpoints). Egress is open because it needs to reach S3/Glue/Athena/Secrets Manager over the
# internet via this VPC's internet gateway (no NAT gateway exists), plus the database.
resource "aws_security_group" "export_task" {
  count       = local.export_schedule_enabled ? 1 : 0
  name        = "mtsai-datalake-${var.environment}-export-task"
  description = "Egress-only security group for the scheduled export Fargate task"
  vpc_id      = var.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = var.tags
}

# Additive: lets the export task reach the database over the network, without touching whatever
# other access (e.g. a developer's IP for manual queries) that security group already allows. Uses
# the modern per-rule resource (not aws_security_group_rule) to match mtsai-api-sim's security
# group, which is deliberately built without inline ingress/egress blocks for exactly this reason.
resource "aws_vpc_security_group_ingress_rule" "db_ingress_from_export_task" {
  count                        = local.export_schedule_enabled && var.db_security_group_id != "" ? 1 : 0
  security_group_id            = var.db_security_group_id
  referenced_security_group_id = aws_security_group.export_task[0].id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"
  description                  = "Postgres from the scheduled export Fargate task"
}

data "aws_iam_policy_document" "scheduler_trust" {
  count = local.export_schedule_enabled ? 1 : 0

  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "scheduler" {
  count              = local.export_schedule_enabled ? 1 : 0
  name               = "mtsai-datalake-${var.environment}-export-scheduler"
  assume_role_policy = data.aws_iam_policy_document.scheduler_trust[0].json
  tags               = var.tags
}

data "aws_iam_policy_document" "scheduler_run_task" {
  count = local.export_schedule_enabled ? 1 : 0

  statement {
    actions   = ["ecs:RunTask"]
    resources = [replace(aws_ecs_task_definition.export.arn, "/:\\d+$/", ":*")]
    condition {
      test     = "ArnLike"
      variable = "ecs:cluster"
      values   = [aws_ecs_cluster.export.arn]
    }
  }

  statement {
    actions   = ["iam:PassRole"]
    resources = [aws_iam_role.execution.arn, aws_iam_role.task.arn]
  }
}

resource "aws_iam_role_policy" "scheduler_run_task" {
  count  = local.export_schedule_enabled ? 1 : 0
  name   = "run-export-task"
  role   = aws_iam_role.scheduler[0].id
  policy = data.aws_iam_policy_document.scheduler_run_task[0].json
}

resource "aws_scheduler_schedule" "nightly_export" {
  count      = local.export_schedule_enabled ? 1 : 0
  name       = "mtsai-datalake-${var.environment}-nightly-export"
  group_name = "default"

  flexible_time_window {
    mode = "OFF"
  }

  schedule_expression = var.schedule_expression

  target {
    arn      = aws_ecs_cluster.export.arn
    role_arn = aws_iam_role.scheduler[0].arn

    ecs_parameters {
      task_definition_arn = aws_ecs_task_definition.export.arn
      launch_type         = "FARGATE"

      network_configuration {
        subnets         = var.subnet_ids
        security_groups = [aws_security_group.export_task[0].id]
        # true because these are public subnets with no NAT gateway (same constraint documented
        # for mtsai-api-sim) - without a public IP the task has no route to the internet at all
        # and every AWS API call would just hang.
        assign_public_ip = true
      }
    }
  }
}

# --- Failure alerting ---

resource "aws_sns_topic" "export_alarms" {
  name = "mtsai-datalake-${var.environment}-export-alarms"
  tags = var.tags
}

resource "aws_sns_topic_subscription" "export_alarms_email" {
  count     = var.alarm_email != "" ? 1 : 0
  topic_arn = aws_sns_topic.export_alarms.arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

resource "aws_cloudwatch_event_rule" "task_stopped" {
  name = "mtsai-datalake-${var.environment}-export-task-stopped"

  # Matches a stopped task that either failed to launch, or whose essential container exited
  # non-zero - EventBridge matches "containers.exitCode" against any element of the array.
  event_pattern = jsonencode({
    source      = ["aws.ecs"]
    detail-type = ["ECS Task State Change"]
    detail = {
      clusterArn = [aws_ecs_cluster.export.arn]
      lastStatus = ["STOPPED"]
      stopCode   = ["TaskFailedToStart", "EssentialContainerExited", "ResourceInitializationError"]
      containers = {
        exitCode = [{ "anything-but" = 0 }, { "exists" = false }]
      }
    }
  })

  tags = var.tags
}

resource "aws_cloudwatch_event_target" "task_stopped_to_sns" {
  rule = aws_cloudwatch_event_rule.task_stopped.name
  arn  = aws_sns_topic.export_alarms.arn
}

resource "aws_sns_topic_policy" "allow_eventbridge" {
  arn = aws_sns_topic.export_alarms.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "AllowEventBridgePublish"
      Effect    = "Allow"
      Principal = { Service = "events.amazonaws.com" }
      Action    = "sns:Publish"
      Resource  = aws_sns_topic.export_alarms.arn
    }]
  })
}

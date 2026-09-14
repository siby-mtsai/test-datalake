# Scheduled ECS Fargate export job + its IAM, schedule, and failure alerting
# (brief Section 4 "Export" row, Section 6, Section 7 Phase 1 step 2 / Phase 2).
#
# The Go export binary itself (mtsai-datalake-export) doesn't exist yet - that's Phase 2. This
# module stands up the AWS-side scaffolding now (cluster, task definition, schedule, IAM, alarms)
# with a placeholder container image, per Phase 1's "IAM roles for export... EventBridge schedule,
# CloudWatch alarms and an SNS topic" scope.

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
  statement {
    sid       = "WriteRawAndManifests"
    actions   = ["s3:PutObject", "s3:GetObject", "s3:ListBucket"]
    resources = ["${var.bucket_arn}/raw/*", "${var.bucket_arn}/export-manifests/*", var.bucket_arn]
  }

  statement {
    sid       = "EncryptWrites"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [var.kms_key_arn]
  }

  statement {
    sid = "CommitToIceberg"
    actions = [
      "athena:StartQueryExecution",
      "athena:GetQueryExecution",
      "athena:GetQueryResults",
      "glue:GetDatabase",
      "glue:GetTable",
      "glue:GetTables",
      "glue:GetPartitions",
      "glue:CreateTable",
      "glue:UpdateTable",
      "glue:BatchCreatePartition",
    ]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "aws:ResourceTag/Environment"
      values   = [var.environment]
    }
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
# Fargate tasks need a VPC network config (subnets + security groups) to run at all, and neither
# is confirmed yet (brief Section 7 Phase 0 - VPC/network not yet decided). Rather than block the
# rest of Phase 1 (bucket, Glue, Athena workgroups) on that, the schedule itself - and the IAM role
# that only exists to run it - are only created once both are actually supplied.

locals {
  export_schedule_enabled = length(var.subnet_ids) > 0 && length(var.security_group_ids) > 0
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
        subnets          = var.subnet_ids
        security_groups  = var.security_group_ids
        assign_public_ip = false
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

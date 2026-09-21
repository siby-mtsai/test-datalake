output "cluster_arn" {
  value = aws_ecs_cluster.export.arn
}

output "task_definition_arn" {
  value = aws_ecs_task_definition.export.arn
}

output "curate_task_definition_arn" {
  value = aws_ecs_task_definition.curate.arn
}

output "compact_task_definition_arn" {
  value = aws_ecs_task_definition.compact.arn
}

output "erasure_task_definition_arn" {
  value = aws_ecs_task_definition.erasure.arn
}

output "export_task_security_group_id" {
  description = "Security group used by both the export and curate Fargate tasks - null until the schedule is enabled."
  value       = local.export_schedule_enabled ? aws_security_group.export_task[0].id : null
}

output "task_role_arn" {
  value = aws_iam_role.task.arn
}

output "alarm_topic_arn" {
  value = aws_sns_topic.export_alarms.arn
}

output "export_schedule_enabled" {
  description = "Whether the nightly EventBridge schedule was created - false until vpc_id and subnet_ids are both populated."
  value       = local.export_schedule_enabled
}

output "ecr_repository_url" {
  value = aws_ecr_repository.export.repository_url
}

output "pipeline_workgroup_name" {
  value = aws_athena_workgroup.pipeline.name
}

output "log_group_name" {
  value = aws_cloudwatch_log_group.export.name
}

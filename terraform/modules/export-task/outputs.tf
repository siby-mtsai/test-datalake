output "cluster_arn" {
  value = aws_ecs_cluster.export.arn
}

output "task_definition_arn" {
  value = aws_ecs_task_definition.export.arn
}

output "task_role_arn" {
  value = aws_iam_role.task.arn
}

output "alarm_topic_arn" {
  value = aws_sns_topic.export_alarms.arn
}

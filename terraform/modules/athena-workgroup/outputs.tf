output "workgroup_names" {
  value = { for k, v in aws_athena_workgroup.consumer : k => v.name }
}

output "consumer_role_arns" {
  value = { for k, v in aws_iam_role.consumer : k => v.arn }
}

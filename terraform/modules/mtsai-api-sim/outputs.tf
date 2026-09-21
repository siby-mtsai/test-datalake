output "endpoint" {
  value = aws_db_instance.this.address
}

output "port" {
  value = aws_db_instance.this.port
}

output "db_name" {
  value = var.db_name
}

output "secret_arn" {
  value = aws_secretsmanager_secret.credentials.arn
}

output "instance_id" {
  value = aws_db_instance.this.id
}

output "security_group_id" {
  value = aws_security_group.db.id
}

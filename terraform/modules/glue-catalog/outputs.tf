output "raw_database_name" {
  value = aws_glue_catalog_database.raw.name
}

output "curated_database_name" {
  value = aws_glue_catalog_database.curated.name
}

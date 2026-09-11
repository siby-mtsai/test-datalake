variable "environment" {
  description = "Environment name (dev, test, preprod, prod)."
  type        = string
}

variable "tags" {
  description = "Tags applied to every resource in this module."
  type        = map(string)
}

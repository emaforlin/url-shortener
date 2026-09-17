variable "region" {
  description = "AWS region where the state bucket is created."
  type        = string
  default     = "us-east-1"
}

variable "project" {
  description = "Project name, used as prefix of the state bucket name."
  type        = string
  default     = "url-shortener"
}

variable "noncurrent_version_retention_days" {
  description = "Days to keep old versions of the state files before expiring them."
  type        = number
  default     = 90
}

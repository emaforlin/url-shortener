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

variable "github_repository" {
  description = "GitHub repository allowed to assume the CI roles, as owner/name."
  type        = string
  default     = "emaforlin/url-shortener"

  validation {
    condition     = can(regex("^[^/]+/[^/]+$", var.github_repository))
    error_message = "github_repository must be owner/name, with no leading or trailing slash."
  }
}

variable "infra_environment" {
  description = "GitHub environment the infra apply runs in. Its name is part of the OIDC subject gh-infra trusts."
  type        = string
  default     = "infra"
}

variable "production_environment" {
  description = "GitHub environment the deploy runs in. Its name is part of the OIDC subject gh-deploy trusts."
  type        = string
  default     = "production"
}

variable "state_key" {
  description = "Object key of the main stack's state file. Must match the `key` in infra/main/terraform.tf."
  type        = string
  default     = "url-shortener/terraform.tfstate"
}

variable "api_token_parameter_name" {
  description = <<-EOT
    Name of the SSM SecureString holding the API token (spec 005). This stack
    only grants access to it; the parameter itself is created by hand and never
    by Terraform, because every Terraform resource that can read a value also
    writes it into state.
  EOT
  type        = string
  default     = "/url-shortener/api-token"

  validation {
    condition     = startswith(var.api_token_parameter_name, "/")
    error_message = "api_token_parameter_name must start with a slash, e.g. /url-shortener/api-token."
  }
}

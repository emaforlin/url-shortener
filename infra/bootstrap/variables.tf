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

variable "github_subject_prefix" {
  description = <<-EOT
    The repository part of the OIDC `sub` claim, which the CI role trust
    policies match exactly.

    Repositories created after 2026-07-15 use GitHub's *immutable* subject
    format, which carries the numeric owner and repository IDs alongside their
    names: `repo:owner@<owner-id>/name@<repo-id>`. The IDs are what make it
    immutable — renaming the owner or the repository changes the names in the
    claim but not the IDs, and a trust policy pinned to the old plain
    `repo:owner/name` form stops matching the moment the feature is on.

    Read the current value straight from GitHub rather than assembling it:

        gh api repos/<owner>/<name>/actions/oidc/customization/sub \
          --jq .sub_claim_prefix
  EOT
  type        = string
  default     = "repo:emaforlin@32603957/url-shortener@1364122573"

  validation {
    condition     = can(regex("^repo:[^/:]+@[0-9]+/[^/:]+@[0-9]+$", var.github_subject_prefix))
    error_message = "github_subject_prefix must look like repo:owner@123/name@456, with no trailing context segment."
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

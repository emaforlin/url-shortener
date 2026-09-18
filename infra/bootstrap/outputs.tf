output "state_bucket_name" {
  description = "Name of the S3 bucket holding the Terraform remote state."
  value       = aws_s3_bucket.state.id
}

output "state_bucket_arn" {
  description = "ARN of the state bucket, for IAM policies of the deploy role."
  value       = aws_s3_bucket.state.arn
}

output "github_oidc_provider_arn" {
  description = "ARN of the GitHub OIDC identity provider the CI roles trust."
  value       = aws_iam_openid_connect_provider.github.arn
}

output "gh_plan_role_arn" {
  description = "Role assumed by the plan job in infra.yml."
  value       = aws_iam_role.ci["plan"].arn
}

output "gh_infra_role_arn" {
  description = "Role assumed by the apply job in infra.yml."
  value       = aws_iam_role.ci["infra"].arn
}

output "gh_deploy_role_arn" {
  description = "Role assumed by the deploy job in deploy.yml."
  value       = aws_iam_role.ci["deploy"].arn
}

output "backend_config" {
  description = "Backend block to paste into infra/terraform.tf."
  value       = <<-EOT
    backend "s3" {
      bucket       = "${aws_s3_bucket.state.id}"
      key          = "url-shortener/terraform.tfstate"
      region       = "${var.region}"
      encrypt      = true
      use_lockfile = true
    }
  EOT
}

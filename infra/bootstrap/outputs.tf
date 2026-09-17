output "state_bucket_name" {
  description = "Name of the S3 bucket holding the Terraform remote state."
  value       = aws_s3_bucket.state.id
}

output "state_bucket_arn" {
  description = "ARN of the state bucket, for IAM policies of the deploy role."
  value       = aws_s3_bucket.state.arn
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

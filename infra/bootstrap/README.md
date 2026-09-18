# infra/bootstrap

Creates the S3 bucket that stores the remote state of `infra/`. It is the only
stack with local state, and it is applied **once, by hand, with admin
credentials** — never from CI.

The bucket has versioning, SSE-S3 encryption, public access blocked, TLS-only
policy and `prevent_destroy`. Locking uses S3 native lockfiles
(`use_lockfile`), so no DynamoDB table is needed.

## Apply

```sh
export AWS_PROFILE=<admin-profile>
cd infra/bootstrap
terraform init
terraform plan  -var region=us-east-1
terraform apply -var region=us-east-1
terraform output -raw backend_config
```

Paste the printed `backend "s3"` block inside the `terraform {}` block of
`infra/terraform.tf`, then run `terraform init` in `infra/`.

## Local state

`terraform.tfstate` is gitignored. If it is lost, recover it by importing the
bucket and its sub-resources (e.g.
`terraform import aws_s3_bucket.state <bucket-name>`); nothing else lives here.

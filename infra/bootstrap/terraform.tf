# Bootstrap stack: creates the S3 bucket that holds the remote state of the
# main infra stack. It is applied once, by hand, with admin credentials, and
# keeps its own state locally (it cannot live in the bucket it creates).
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  # 1.10+ for S3 native state locking (use_lockfile) in the main stack.
  required_version = ">= 1.10"
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project   = var.project
      ManagedBy = "terraform"
      Stack     = "bootstrap"
    }
  }
}

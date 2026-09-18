terraform {
  backend "s3" {
    bucket       = "url-shortener-tfstate-637423436643-us-east-1"
    key          = "url-shortener/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }

    # Builds the placeholder deployment package. See lambda.tf for why the
    # function is not created from the real build output.
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.4"
    }
  }

  # 1.10+ for S3 native state locking (use_lockfile) above.
  required_version = ">= 1.10"
}

provider "aws" {
  region = var.region

  # Tag keys are case-sensitive, and these two are the ones the spec names.
  # Everything the provider creates carries them, so a cost report or a stray
  # resource hunt can filter on them without every resource repeating the block.
  default_tags {
    tags = {
      project    = var.project
      managed-by = "terraform"
    }
  }
}

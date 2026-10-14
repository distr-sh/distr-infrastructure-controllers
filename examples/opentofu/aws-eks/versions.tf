terraform {
  required_version = ">= 1.10"

  # The OpenTofu controller passes the path to the state in its data directory.
  backend "local" {}

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.67.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "3.9.1"
    }
  }
}

# Credentials come from the environment: the instance profile of the management EC2 instance, EKS Pod
# Identity or IRSA of the controller's service account, or AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.
provider "aws" {
  region = var.region

  default_tags {
    tags = local.tags
  }
}


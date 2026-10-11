terraform {
  required_version = ">= 1.10"

  # The OpenTofu controller passes the path to the state in its data directory.
  backend "local" {}

  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "3.9.1"
    }
    time = {
      source  = "hashicorp/time"
      version = "0.14.2"
    }
  }
}

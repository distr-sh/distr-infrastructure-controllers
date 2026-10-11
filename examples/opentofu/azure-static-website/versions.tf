terraform {
  required_version = ">= 1.10"

  # The OpenTofu controller passes the path to the state in its data directory.
  backend "local" {}

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "5.8.0"
    }
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

# Credentials and the subscription come from the environment: ARM_SUBSCRIPTION_ID plus ARM_USE_MSI=true for
# the managed identity of the management VM, ARM_USE_AKS_WORKLOAD_IDENTITY=true for the OpenTofu controller pod, or
# ARM_CLIENT_ID, ARM_CLIENT_SECRET and ARM_TENANT_ID of a service principal for a quick test.
provider "azurerm" {
  features {
    resource_group {
      # Everything in the group belongs to this configuration.
      prevent_deletion_if_contains_resources = false
    }
  }
}

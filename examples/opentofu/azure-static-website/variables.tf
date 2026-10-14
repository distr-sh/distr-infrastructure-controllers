variable "instance_name" {
  description = "Name of this installation, set by the OpenTofu controller from INSTANCE_NAME."
  type        = string
}

variable "app_version" {
  description = "Version of the infrastructure application, set by the OpenTofu controller."
  type        = string
}

variable "location" {
  description = "Azure region of the resource group and the storage account."
  type        = string
  default     = "westeurope"
}

variable "title" {
  description = "Title of the website."
  type        = string
  default     = "Hello from Distr BYOC"
}

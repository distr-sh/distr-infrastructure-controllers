variable "instance_name" {
  description = "Name of this installation, set by the OpenTofu controller from INSTANCE_NAME."
  type        = string
}

variable "app_version" {
  description = "Version of the infrastructure application, set by the OpenTofu controller."
  type        = string
}

variable "region" {
  description = "AWS region of the installation."
  type        = string
  default     = "eu-central-1"
}

variable "kubernetes_version" {
  description = "Kubernetes version of the EKS cluster."
  type        = string
  default     = "1.35"
}

variable "endpoint_public_access_cidrs" {
  description = "Networks that reach the Kubernetes API."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "node_instance_types" {
  description = "Instance types of the managed node group."
  type        = list(string)
  default     = ["t3.large"]
}

variable "node_count" {
  description = "Number of nodes of the managed node group."
  type        = number
  default     = 2
}

variable "database_instance_class" {
  description = "Instance class of the PostgreSQL database."
  type        = string
  default     = "db.t4g.micro"
}

variable "database_deletion_protection" {
  description = "Protect the database from being deleted. Set to false before destroying the installation."
  type        = bool
  default     = true
}

variable "application_namespace" {
  description = "Namespace of the application that gets access to the bucket through EKS Pod Identity."
  type        = string
  default     = "app"
}

variable "application_service_account" {
  description = "Service account of the application that gets access to the bucket through EKS Pod Identity."
  type        = string
  default     = "app"
}

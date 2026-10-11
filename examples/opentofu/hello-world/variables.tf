variable "instance_name" {
  description = "Name of this installation, set by the OpenTofu controller from INSTANCE_NAME."
  type        = string
}

variable "app_version" {
  description = "Version of the infrastructure application, set by the OpenTofu controller."
  type        = string
}

variable "greeting" {
  description = "Greeting returned in the outputs."
  type        = string
  default     = "Hello"
}

variable "provisioning_duration" {
  description = "How long creating the fake infrastructure takes, to show the progress logs of a long apply."
  type        = string
  default     = "45s"
}

output "cluster_name" {
  description = "Name of the EKS cluster."
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "Endpoint of the Kubernetes API of the EKS cluster."
  value       = module.eks.cluster_endpoint
}

output "database_endpoint" {
  description = "Endpoint of the PostgreSQL database."
  value       = aws_db_instance.database.endpoint
}

output "bucket_name" {
  description = "Name of the bucket of the application."
  value       = aws_s3_bucket.data.bucket
}

output "kubeconfig_command" {
  description = "Command that writes a kubeconfig for the cluster."
  value       = "aws eks update-kubeconfig --region ${var.region} --name ${module.eks.cluster_name}"
}

output "app_version" {
  description = "Version of the infrastructure application that applied the configuration."
  value       = var.app_version
}

output "greeting" {
  description = "Greeting from the fake cluster."
  value       = "${var.greeting} from ${random_pet.cluster.id}"
}

output "app_version" {
  description = "Version of the infrastructure application that was applied last."
  value       = terraform_data.release.output.app_version
}

output "released_at" {
  description = "When the current version was applied for the first time."
  value       = time_static.released_at.rfc3339
}

output "database_password" {
  description = "A generated secret, to show that sensitive outputs are not logged."
  value       = random_password.database.result
  sensitive   = true
}

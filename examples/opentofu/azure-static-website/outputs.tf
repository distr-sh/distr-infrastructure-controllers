output "website_url" {
  description = "URL of the website."
  value       = azurerm_storage_account.website.primary_web_endpoint
}

output "version_url" {
  description = "URL of version.json, which tells the deployed version."
  value       = "${trimsuffix(azurerm_storage_account.website.primary_web_endpoint, "/")}/version.json"
}

output "storage_account_name" {
  description = "Name of the storage account holding the website."
  value       = azurerm_storage_account.website.name
}

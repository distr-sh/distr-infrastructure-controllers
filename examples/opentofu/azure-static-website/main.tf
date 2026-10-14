# A static website in an Azure storage account that shows which version of the infrastructure application
# is deployed. Every new version rewrites index.html and version.json, which makes a rollout through Distr
# visible from outside.

locals {
  tags = {
    "distr.sh-instance"   = var.instance_name
    "distr.sh-managed-by" = "distr-opentofu-controller"
  }

  # Storage account names are global, 3 to 24 lowercase letters and digits.
  account_name = "${substr(replace(var.instance_name, "-", ""), 0, 16)}${random_string.account.result}"

  # The static website configuration creates the $web container.
  web_container_id = "${azurerm_storage_account.website.id}/blobServices/default/containers/$web"

  release = {
    app_version   = var.app_version
    instance_name = var.instance_name
    released_at   = time_static.released_at.rfc3339
  }
}

resource "random_string" "account" {
  length  = 8
  upper   = false
  special = false
}

resource "time_static" "released_at" {
  triggers = {
    app_version = var.app_version
  }
}

resource "azurerm_resource_group" "website" {
  name     = "${var.instance_name}-website"
  location = var.location
  tags     = local.tags
}

resource "azurerm_storage_account" "website" {
  name                     = local.account_name
  resource_group_name      = azurerm_resource_group.website.name
  location                 = azurerm_resource_group.website.location
  account_kind             = "StorageV2"
  account_tier             = "Standard"
  account_replication_type = "LRS"
  min_tls_version          = "TLS1_2"

  # The website endpoint serves the $web container. Nothing else is public.
  allow_nested_items_to_be_public = false
  https_traffic_only_enabled      = true

  tags = local.tags
}

resource "azurerm_storage_account_static_website" "website" {
  storage_account_id = azurerm_storage_account.website.id
  index_document     = "index.html"
  error_404_document = "404.html"
}

resource "azurerm_storage_blob" "index" {
  name                 = "index.html"
  storage_container_id = local.web_container_id
  type                 = "Block"
  content_type         = "text/html; charset=utf-8"
  source_content       = templatefile("${path.module}/templates/index.html.tftpl", merge(local.release, { title = var.title }))

  depends_on = [azurerm_storage_account_static_website.website]
}

resource "azurerm_storage_blob" "not_found" {
  name                 = "404.html"
  storage_container_id = local.web_container_id
  type                 = "Block"
  content_type         = "text/html; charset=utf-8"
  source_content       = templatefile("${path.module}/templates/404.html.tftpl", { title = var.title })

  depends_on = [azurerm_storage_account_static_website.website]
}

resource "azurerm_storage_blob" "version" {
  name                 = "version.json"
  storage_container_id = local.web_container_id
  type                 = "Block"
  content_type         = "application/json"
  source_content       = jsonencode(local.release)

  depends_on = [azurerm_storage_account_static_website.website]
}

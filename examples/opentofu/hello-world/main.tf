# Infrastructure without a cloud: a few resources of the random and time providers that behave like real
# ones. Creating them takes a while, a new version changes one of them in place and they keep their
# identity across restarts as long as the state survives.

resource "random_pet" "cluster" {
  prefix = var.instance_name
  length = 2
}

resource "time_sleep" "provisioning" {
  create_duration = var.provisioning_duration

  triggers = {
    cluster = random_pet.cluster.id
  }
}

resource "random_password" "database" {
  length  = 32
  special = false

  depends_on = [time_sleep.provisioning]
}

# Changes in place whenever a new version of the infrastructure application rolls out.
resource "terraform_data" "release" {
  input = {
    app_version = var.app_version
    cluster     = random_pet.cluster.id
  }
}

resource "time_static" "released_at" {
  triggers = {
    app_version = var.app_version
  }
}

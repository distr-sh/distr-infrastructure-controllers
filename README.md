# distr-controller-contrib

Controllers for Distr that are not built into Distr (yet). Distr deploys each of them like any other
application, with the Docker controller on a VM or with the Kubernetes controller in a cluster, and rolls
out new versions of what they manage the same way it rolls out new versions of an application.

| Controller                                  | Manages                                                   | Image                                                           |
| ------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------------- |
| [OpenTofu controller](#opentofu-controller) | The infrastructure of a Bring Your Own Cloud installation | `ghcr.io/distr-sh/distr-controller-contrib/opentofu-controller` |

Every controller follows the same rules:

- Everything it does is visible in its logs, in sentences a vendor understands, because the deployment
  logs in Distr are the only window a vendor has into it.
- Its state lives in a volume: a named volume on Docker, a retained volume claim on Kubernetes.
- It deletes nothing it manages without an explicit confirmation.
- It reports its health on `/healthz` and `/readyz`, which makes a failure visible as an unhealthy
  deployment in Distr.

## OpenTofu controller

The OpenTofu controller provisions the infrastructure of a Bring Your Own Cloud installation with
OpenTofu. Distr deploys it with the Docker controller on a management VM or with the Kubernetes
controller in a management cluster.

The infrastructure itself is an OpenTofu module package in an OCI registry, such as the Distr registry or
GHCR. Every `RECONCILE_INTERVAL`, the controller checks `MODULE_SOURCE` for a new package, plans and
applies it, keeps the state in a volume and logs every step, so the deployment logs in Distr tell what
is being created, changed or destroyed, and why.

```text
2026-10-08T06:58:58.838Z INFO run #1 started {"operation": "apply", "mode": "auto", "version": "0.2.0"}
2026-10-08T06:58:58.861Z INFO downloading the module oci://ghcr.io/acme/modules/website?tag=0.2.0 (sha256:4f1c…)
2026-10-08T06:58:58.870Z INFO initializing the working directory
2026-10-08T06:59:01.112Z INFO plan: update aws_s3_object.version
2026-10-08T06:59:01.112Z INFO plan: 0 to add, 1 to change, 0 to destroy
2026-10-08T06:59:01.115Z INFO upgrading the infrastructure from version 0.1.0 to 0.2.0
2026-10-08T06:59:02.301Z INFO aws_s3_object.version: modifying
2026-10-08T06:59:02.512Z INFO aws_s3_object.version: modification complete after 0s [id=version.json]
2026-10-08T06:59:02.530Z INFO apply complete: 0 added, 1 changed, 0 destroyed
2026-10-08T06:59:02.531Z INFO output version_url = "http://acme-website-3f2a1b9c.s3-website.eu-central-1.amazonaws.com/version.json"
2026-10-08T06:59:02.540Z INFO run #1 finished in 3.7s: the infrastructure is up to date
```

### How a vendor ships infrastructure

1. Write an OpenTofu configuration with an empty `backend "local" {}` and a dependency lock file that
   covers `linux_amd64` and `linux_arm64` (`mise run opentofu:lock:tofu`).
2. Package it and push it as an
   [OpenTofu module package](https://opentofu.org/docs/language/modules/sources/#oci-registry):

   ```sh
   hack/package-module.sh path/to/config module.zip
   oras push --artifact-type application/vnd.opentofu.modulepkg registry.example.com/acme/modules/app:1.0.0 module.zip:archive/zip
   ```

3. Release a version in Distr whose Compose file or chart runs the image
   `ghcr.io/distr-sh/distr-controller-contrib/opentofu-controller` with
   `MODULE_SOURCE=oci://registry.example.com/acme/modules/app?tag=1.0.0`. Without `APP_VERSION`, the tag
   is the version.

The package has to contain the lock file, since the controller installs exactly the locked providers.
`hack/package-module.sh` packages equal content into an equal package, so pushing it again does not make
the controllers download it again. The controller remembers the digest of the package it applied, which
also catches a tag that was moved to another package.

For installations without access to `registry.opentofu.org`, push the providers to the same registry and
set `PROVIDER_MIRROR`:

```sh
hack/push-providers.sh path/to/config registry.example.com/acme/opentofu-providers
```

### Examples

Every directory in [`examples/opentofu/`](examples/opentofu) is a complete infrastructure application:

| Example                                                          | Provisions                                                           |
| ---------------------------------------------------------------- | -------------------------------------------------------------------- |
| [`hello-world`](examples/opentofu/hello-world)                   | Nothing real: random names, a password and a fake provisioning delay |
| [`azure-static-website`](examples/opentofu/azure-static-website) | A public Azure Storage website showing the deployed version          |
| [`aws-eks`](examples/opentofu/aws-eks)                           | An EKS cluster, a PostgreSQL database and a bucket                   |

An example consists of:

- `*.tf` and `.terraform.lock.hcl`: the OpenTofu configuration, released as the module package
  `ghcr.io/distr-sh/distr-controller-contrib/opentofu-controller/modules/<example>`.
- `deploy/docker/`: the Compose file and `env.template` of a Docker deployment in Distr.
- `deploy/charts/<example>/`: the Helm chart of a Kubernetes deployment in Distr, released as
  `oci://ghcr.io/distr-sh/distr-controller-contrib/opentofu-controller/charts/<example>`.

To build an infrastructure application of your own, copy an example and change its OpenTofu configuration.

### Configuration

| Variable                                 | Default           | Description                                                                     |
| ---------------------------------------- | ----------------- | ------------------------------------------------------------------------------- |
| `INSTANCE_NAME`                          | required          | Name of the installation, passed to OpenTofu as `var.instance_name`             |
| `MODULE_SOURCE`                          |                   | `oci://REGISTRY/REPOSITORY[//SUBDIR]?tag=TAG` or `?digest=DIGEST` of the module |
| `APP_VERSION`                            | the tag           | Version of the infrastructure, passed to OpenTofu as `var.app_version`          |
| `PROVIDER_MIRROR`                        |                   | OCI repository prefix to install the providers of `registry.opentofu.org` from  |
| `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` |                   | Credentials for the registries of the module and the providers                  |
| `APPLY_MODE`                             | `auto`            | `auto` applies every plan, `plan` only logs it for a review, except a destroy   |
| `RECONCILE_INTERVAL`                     | `15m`             | Time between two runs                                                           |
| `RETRY_INTERVAL`                         | `2m`              | Time before the next run after a failed run                                     |
| `STATE_ENCRYPTION_PASSPHRASE`            |                   | Encrypts the state and the plans, at least 16 characters                        |
| `DESTROY_CONFIRM`                        |                   | Set to `INSTANCE_NAME` to destroy all infrastructure                            |
| `HEALTH_ADDR`                            | `:8686`           | Address of `/healthz` and `/readyz`, off the ports Distr uses                   |
| `LOG_LEVEL`, `LOG_FORMAT`                | `info`, `console` | `debug`, `info`, `warn`, `error`; `console` or `json`                           |
| `TF_VAR_<name>`                          |                   | Variables of the OpenTofu configuration                                         |

Without `MODULE_SOURCE`, the configuration has to be in `WORKING_DIR` already, which is how
`mise run opentofu:run` runs an example locally. Without the registry credentials, the controller and OpenTofu use
those of a Docker configuration file, such as `~/.docker/config.json`. A registry with a private
certificate authority needs it in `SSL_CERT_DIR`.

The controller compares the version, the digest of the module package and the `TF_VAR_` variables with
those of the last apply, which tells an upgrade, a changed package, a configuration change and drift apart
in the logs. The module package is kept in the data directory, so when the registry cannot be reached, the runs
go on with the package they already have, also after a restart.

The providers are cached in the data directory, so that OpenTofu downloads every provider version only
once. Versions that the module no longer uses are removed from the cache. The AWS provider alone takes
about 800 MB.

`/healthz` reports whether the controller is alive and `/readyz` whether its last run succeeded, which
makes a failed run visible as an unhealthy deployment in Distr.

### Deployment

- Docker: the state lives in the named volume `data` of the deployment, which the Distr Docker controller
  removes when the application is undeployed. Destroy the infrastructure with `DESTROY_CONFIRM` before undeploying it,
  or it is left without a state.
- Kubernetes: a StatefulSet whose volume claim outlives the release.

## Development

All tools are installed with [mise](https://mise.jdx.dev). The OpenTofu controller and its examples are
built, tested and released by the `opentofu-controller` and `opentofu-controller-examples` workflows.

```sh
mise run opentofu:build
mise run opentofu:run
mise run test
mise run lint
mise run format
mise run opentofu:lock:tofu
mise run opentofu:package:module
```

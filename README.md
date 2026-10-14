# Distr infrastructure controllers

Infrastructure controllers for Distr: they provision and manage the infrastructure an application runs on.
Distr deploys each of them like any other application, with the Docker controller on a VM or with the
Kubernetes controller in a cluster, and rolls out new versions of what they manage the same way it rolls out
new versions of an application.

| Controller                                  | Manages                                                   | Image                                                                   |
| ------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------- |
| [OpenTofu controller](#opentofu-controller) | The infrastructure of a Bring Your Own Cloud installation | `ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller` |

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

The infrastructure itself is an OpenTofu configuration that ships in an image built FROM the OpenTofu
controller image. On every start the controller copies the configuration into its data volume, next to the
state. Every `RECONCILE_INTERVAL` it plans and applies the configuration and logs every step, so the
deployment logs in Distr tell what is being created, changed or destroyed, and why. A new version of the
infrastructure is a new image, which Distr rolls out like any other.

```text
2026-10-08T06:58:58.838Z INFO run #1 started {"operation": "apply", "mode": "auto", "version": "0.2.0"}
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

1. Write an OpenTofu configuration with an empty `backend "local" {}` and the dependency lock file that
   `tofu init` writes.
2. Build an image FROM the OpenTofu controller image that copies the configuration to `/module`, and push
   it to a registry such as the Distr registry or GHCR:

   ```dockerfile
   # x-release-please-start-version
   FROM ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller:0.1.0
   # x-release-please-end
   ENV APP_VERSION=1.0.0
   COPY *.tf .terraform.lock.hcl /module/
   ```

3. Release a version in Distr whose Compose file or chart runs that image with a volume at `/data`.

The image has to contain the lock file, since the controller installs exactly the locked providers. Without
`APP_VERSION`, the version of the OpenTofu controller is the version of the infrastructure. The controller
remembers a digest of the configuration it applied, which tells a new configuration of the same version
apart from drift.

OpenTofu downloads the providers when a run needs them. For installations without access to
`registry.opentofu.org`, push the providers for `linux_amd64` and `linux_arm64` to an OCI registry as
described in [Provider Mirrors in OCI Registries](https://opentofu.org/docs/cli/oci_registries/provider-mirror/),
one repository per provider such as `registry.example.com/acme/opentofu-providers/hashicorp/aws`, and set
`PROVIDER_MIRROR=registry.example.com/acme/opentofu-providers`.

### Examples

Every directory in [`examples/opentofu/`](examples/opentofu) is a complete infrastructure application:

| Example                                                          | Provisions                                                           |
| ---------------------------------------------------------------- | -------------------------------------------------------------------- |
| [`hello-world`](examples/opentofu/hello-world)                   | Nothing real: random names, a password and a fake provisioning delay |
| [`azure-static-website`](examples/opentofu/azure-static-website) | A public Azure Storage website showing the deployed version          |
| [`aws-eks`](examples/opentofu/aws-eks)                           | An EKS cluster, a PostgreSQL database and a bucket in one zone       |

An example consists of:

- `*.tf` and `.terraform.lock.hcl`: the OpenTofu configuration.
- `Dockerfile`: the image with the configuration, built FROM the OpenTofu controller image and released as
  `ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller/examples/<example>`.
- `deploy/docker/`: the Compose file and `env.template` of a Docker deployment in Distr.
- `deploy/charts/<example>/`: the Helm chart of a Kubernetes deployment in Distr, released as
  `oci://ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller/charts/<example>`.

To build an infrastructure application of your own, copy an example and change its OpenTofu configuration.

### Configuration

| Variable                                 | Default                | Description                                                                    |
| ---------------------------------------- | ---------------------- | ------------------------------------------------------------------------------ |
| `INSTANCE_NAME`                          | required               | Name of the installation, passed to OpenTofu as `var.instance_name`            |
| `MODULE_DIR`                             | `/module`              | Directory of the OpenTofu configuration in the image                           |
| `APP_VERSION`                            | the controller version | Version of the infrastructure, passed to OpenTofu as `var.app_version`         |
| `PROVIDER_MIRROR`                        |                        | OCI repository prefix to install the providers of `registry.opentofu.org` from |
| `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` |                        | Credentials for the registry of `PROVIDER_MIRROR`                              |
| `APPLY_MODE`                             | `auto`                 | `auto` applies every plan, `plan` only logs it for a review, except a destroy  |
| `RECONCILE_INTERVAL`                     | `15m`                  | Time between two runs                                                          |
| `RETRY_INTERVAL`                         | `2m`                   | Time before the next run after a failed run                                    |
| `STATE_ENCRYPTION_PASSPHRASE`            |                        | Encrypts the state and the plans, at least 16 characters                       |
| `DESTROY_CONFIRM`                        |                        | Set to `INSTANCE_NAME` to destroy all infrastructure                           |
| `HEALTH_ADDR`                            | `:8686`                | Address of `/healthz` and `/readyz`, off the ports Distr uses                  |
| `LOG_LEVEL`, `LOG_FORMAT`                | `info`, `console`      | `debug`, `info`, `warn`, `error`; `console` or `json`                          |
| `TF_VAR_<name>`                          |                        | Variables of the OpenTofu configuration                                        |

`mise run opentofu:run` runs an example locally without an image, with `MODULE_DIR` pointing at it. Without
the registry credentials, OpenTofu uses those of a Docker configuration file, such as `~/.docker/config.json`.
A registry with a private certificate authority needs it in `SSL_CERT_DIR`.

The controller compares the version, the digest of the OpenTofu configuration and the `TF_VAR_` variables
with those of the last apply, which tells an upgrade, a changed configuration, changed variables and drift
apart in the logs.

The providers are cached in the data volume, so that OpenTofu downloads every provider version only once,
also across restarts. Versions that the configuration no longer uses are removed from the cache. The AWS
provider alone takes about 800 MB.

`/healthz` reports whether the controller is alive and `/readyz` whether its last run succeeded, which
makes a failed run visible as an unhealthy deployment in Distr.

### Deployment

- Docker: the state, the configuration and the providers live in the named volume `data` of the deployment,
  which the Distr Docker controller removes when the application is undeployed. Destroy the infrastructure with `DESTROY_CONFIRM` before undeploying it,
  or it is left without a state.
- Kubernetes: a StatefulSet whose volume claim outlives the release.

## Development

All tools are installed with [mise](https://mise.jdx.dev). CI runs `mise run lint` in the `lint` workflow,
and the OpenTofu controller and its examples are tested, built and released by the `opentofu-controller` and
`opentofu-controller-examples` workflows.

```sh
mise run opentofu:build
mise run opentofu:run
mise run test
mise run lint
mise run format
mise run opentofu:docker-build:example
```

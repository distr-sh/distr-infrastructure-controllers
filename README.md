# Distr infrastructure controllers

The controllers in this repository are deployed like any other Distr application and manage infrastructure.

| Controller                                  | Manages                                                   | Image                                                                   |
| ------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------- |
| [OpenTofu controller](#opentofu-controller) | The infrastructure of a Bring Your Own Cloud installation | `ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller` |

## OpenTofu controller

The OpenTofu controller provisions the infrastructure of a Bring Your Own Cloud installation with OpenTofu.
The infrastructure is just a Terraform configuration baked into the controller image, and its state lives
in a persistent volume.

When it starts, the controller copies the configuration into its data volume, next to the state. Then it
plans and applies the configuration every `RECONCILE_INTERVAL` and logs each step, so the deployment logs in
Distr show what is being created, changed or destroyed, and why. To ship a new version of the infrastructure,
you release a new image, and Distr rolls it out like any other.

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

### How to deploy infrastructure into a customer's cloud account

1. Write an OpenTofu configuration with an empty `backend "local" {}` block, and keep the
   `.terraform.lock.hcl` that `tofu init` writes.
2. Build an image on top of the OpenTofu controller image that copies the configuration to `/module`, and
   push it to a registry such as the Distr registry or GHCR:

   <!-- x-release-please-start-version -->

   ```dockerfile
   FROM ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller:0.2.0
   ENV APP_VERSION=<version>
   COPY *.tf .terraform.lock.hcl /module/
   ```

   <!-- x-release-please-end -->

3. Release a version in Distr whose Compose file or chart runs that image with a volume at `/data`.

The image must contain the lock file, because the controller installs exactly the providers locked in it.
Build it with `--build-arg VERSION=1.0.0` to set the version of the infrastructure. Without it, the
infrastructure gets the version of the OpenTofu controller. The controller also remembers a digest of the
configuration it applied, so a changed configuration under the same version isn't mistaken for drift.

OpenTofu downloads the providers when a run needs them. If an installation can't reach
`registry.opentofu.org`, push the `linux_amd64` and `linux_arm64` builds of its providers to an OCI registry,
as described in [Provider Mirrors in OCI Registries](https://opentofu.org/docs/cli/oci_registries/provider-mirror/).
Each provider gets its own repository, such as `registry.example.com/acme/opentofu-providers/hashicorp/aws`.
Then set `PROVIDER_MIRROR=registry.example.com/acme/opentofu-providers`.

### Examples

Every directory in [`examples/opentofu/`](examples/opentofu) is a complete infrastructure application:

| Example                                                          | Provisions                                                           |
| ---------------------------------------------------------------- | -------------------------------------------------------------------- |
| [`hello-world`](examples/opentofu/hello-world)                   | Nothing real: random names, a password and a fake provisioning delay |
| [`azure-static-website`](examples/opentofu/azure-static-website) | A public Azure Storage website showing the deployed version          |
| [`aws-eks`](examples/opentofu/aws-eks)                           | An EKS cluster, a PostgreSQL database and a bucket in one zone       |

An example consists of:

- `*.tf` and `.terraform.lock.hcl`: the OpenTofu configuration.
- `Dockerfile`: the image with the configuration, built on the OpenTofu controller image and released as
  `ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller/examples/<example>`.
- `deploy/docker/`: the Compose file and `env.template` of a Docker deployment in Distr.
- `deploy/charts/<example>/`: the Helm chart of a Kubernetes deployment in Distr, released as
  `oci://ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller/charts/<example>`.

To build an infrastructure application of your own, copy an example and change its OpenTofu configuration.

### Configuration

| Variable                                 | Default                | Description                                                                                  |
| ---------------------------------------- | ---------------------- | -------------------------------------------------------------------------------------------- |
| `INSTANCE_NAME`                          | required               | Name of the installation, passed to OpenTofu as `var.instance_name`                          |
| `MODULE_DIR`                             | `/module`              | Directory of the OpenTofu configuration in the image                                         |
| `APP_VERSION`                            | the controller version | Version of the infrastructure, passed to OpenTofu as `var.app_version`                       |
| `PROVIDER_MIRROR`                        |                        | OCI repository prefix to install the providers of `registry.opentofu.org` from               |
| `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` |                        | Credentials for the registry of `PROVIDER_MIRROR`                                            |
| `APPLY_MODE`                             | `auto`                 | `auto` applies every plan, `plan` only logs it for review but still runs a confirmed destroy |
| `RECONCILE_INTERVAL`                     | `15m`                  | Time between two runs                                                                        |
| `RETRY_INTERVAL`                         | `2m`                   | Time before the next run after a failed run                                                  |
| `STATE_ENCRYPTION_PASSPHRASE`            |                        | Encrypts the state and the plans, at least 16 characters                                     |
| `DESTROY_CONFIRM`                        |                        | Set to `INSTANCE_NAME` to destroy all infrastructure                                         |
| `HEALTH_ADDR`                            | `:8686`                | Address of `/healthz` and `/readyz`, by default on a port Distr doesn't use                  |
| `LOG_LEVEL`, `LOG_FORMAT`                | `info`, `console`      | `debug`, `info`, `warn`, `error`; `console` or `json`                                        |
| `TF_VAR_<name>`                          |                        | Variables of the OpenTofu configuration                                                      |

To try an example locally without building an image, run `mise run opentofu:run`, which points `MODULE_DIR`
at the example. Without `REGISTRY_USERNAME` and `REGISTRY_PASSWORD`, OpenTofu uses the credentials from a
Docker configuration file such as `~/.docker/config.json`. For a registry whose certificate comes from a
private certificate authority, put the CA certificate into `SSL_CERT_DIR`.

On every run, the controller compares the version, the configuration digest and the `TF_VAR_` variables with
those of the last apply. The logs then say whether a change comes from an upgrade, a new configuration,
changed variables or drift.

OpenTofu caches the providers in the data volume, so it downloads each provider version only once, even
across restarts. The controller removes versions the configuration no longer uses. The AWS provider alone
takes about 800 MB, so size the volume accordingly.

`/healthz` reports whether the controller is alive, and `/readyz` whether its last run succeeded. So a failed
run shows up in Distr as an unhealthy deployment.

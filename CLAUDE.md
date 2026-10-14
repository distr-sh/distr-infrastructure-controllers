# CLAUDE.md

Guidance for Claude Code (claude.ai/code) and other agents working in this repository.

## Project

Infrastructure controllers for Distr: controllers that provision and manage the infrastructure an
application runs on, installed as ordinary Distr applications rather than built into Distr and deployed by
the Distr Docker or Kubernetes controller. The only controller so far is the OpenTofu controller, a Go
program that runs OpenTofu against the configuration in its image. A vendor builds an image FROM the
controller image that copies the configuration to `/module`, and the controller copies it into its data
volume on every start. Everything a controller does must be visible in its logs, because the deployment logs
in Distr are the only window a vendor has into it.

## Repository layout

- `cmd/controller/opentofu/`: the entrypoint, with the `run`, `healthcheck` and `version` commands.
- `internal/config/`: the environment variables. `internal/module/`: copies the configuration from
  `MODULE_DIR` into the data directory and hashes it. `internal/reconcile/`: the loop that decides between
  apply, plan only and destroy. `internal/tofu/`: runs `tofu`, turns its `-json` output into log lines and
  writes the CLI configuration for a provider mirror and registry credentials. `internal/store/`: the data
  directory with the configuration, the state, the plan, the plugin cache and `status.json`.
  `internal/health/`: `/healthz` and `/readyz`.
- `Dockerfile`: the controller image, without any OpenTofu configuration.
- `examples/opentofu/<name>/`: one infrastructure application each, with its OpenTofu configuration, a
  `Dockerfile` that builds `ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller/examples/<name>`
  FROM the controller image, and `deploy/docker/` and `deploy/charts/<name>/` that run that image. CI
  discovers new examples itself.
- `hack/current-commit.sh`: the commit hash of a build, which mise loads into `COMMIT`.

## Commands

Build, test, lint and format through mise.

```sh
mise run opentofu:build
mise run test
mise run lint
mise run format
mise exec -- tofu -chdir=examples/opentofu/<name> init -upgrade -backend=false # after changing a provider version
```

## Rules

- Log with zap in sentences a vendor understands without knowing OpenTofu: what is happening and why.
  Warnings and errors go to stderr, everything else to stdout.
- The configuration in an image must contain its `.terraform.lock.hcl`, since the controller runs
  `tofu init -lockfile=readonly`.
- The image is the only source of the configuration: the controller replaces the copy in the data volume on
  every start. OpenTofu downloads the providers at runtime into the plugin cache in the data volume.
- The state, the configuration and the providers live in a named volume on Docker, which an undeploy in
  Distr removes, and in a retained volume claim on Kubernetes. Host directories cannot be mounted on Docker.
- Destroying needs `DESTROY_CONFIRM` set to the instance name. Do not add another path that destroys.
- Pin provider and module versions exactly, and keep the examples on the same provider versions.
- Write the commands of a task into `mise.toml` instead of a script in `hack/`, and do not add scripts that
  are only run by hand.
- An example needs a `Dockerfile` and `.dockerignore`, `deploy/docker/` with an `env.template`, a chart and
  an entry in the README table. Add its versioned files to `release-please-config.json`.

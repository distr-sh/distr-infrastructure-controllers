# CLAUDE.md

Guidance for Claude Code (claude.ai/code) and other agents working in this repository.

## Project

Contrib controllers for Distr: controllers installed as ordinary Distr applications rather than built
into Distr, deployed by the Distr Docker or Kubernetes controller. The only controller so far is the OpenTofu
controller, a Go program that downloads an OpenTofu module package from an OCI registry and runs OpenTofu
against it. Everything a controller does must be visible in its logs, because the deployment logs in Distr
are the only window a vendor has into it.

## Repository layout

- `cmd/controller/opentofu/`: the entrypoint, with the `run`, `healthcheck` and `version` commands.
- `internal/config/`: the environment variables. `internal/module/`: parses `MODULE_SOURCE`, resolves its
  tag and downloads and extracts the module package. `internal/reconcile/`: the loop that decides between
  apply, plan only and destroy. `internal/tofu/`: runs `tofu`, turns its `-json` output into log lines and
  writes the CLI configuration for a provider mirror and registry credentials. `internal/store/`: the data
  directory with the state, the plan, the plugin cache and `status.json`.
  `internal/health/`: `/healthz` and `/readyz`.
- `Dockerfile`: the controller image, without any OpenTofu configuration.
- `examples/opentofu/<name>/`: one infrastructure application each, with its OpenTofu configuration, released as
  the module package `ghcr.io/distr-sh/distr-controller-contrib/opentofu-controller/modules/<name>`, and `deploy/docker/`
  and `deploy/charts/<name>/` that run the controller image with it. CI discovers new examples itself.
- `hack/package-module.sh`, `hack/push-providers.sh`: package a configuration, mirror its providers.

## Commands

Build, test, lint and format through mise.

```sh
mise run opentofu:build
mise run test
mise run lint
mise run format
mise run opentofu:lock:tofu   # after changing a provider version
```

## Rules

- Log with zap in sentences a vendor understands without knowing OpenTofu: what is happening and why.
  Warnings and errors go to stderr, everything else to stdout.
- A module package must contain its `.terraform.lock.hcl`, which must cover linux_amd64 and linux_arm64.
  The controller runs `tofu init -lockfile=readonly`.
- A run must not need the registry once the controller has the package: it goes on with the package it
  has when the registry is unreachable.
- The state lives in a named volume on Docker, which an undeploy in Distr removes, and in a retained volume
  claim on Kubernetes. Host directories cannot be mounted on Docker.
- Destroying needs `DESTROY_CONFIRM` set to the instance name. Do not add another path that destroys.
- Pin provider and module versions exactly, and keep the examples on the same provider versions.
- An example needs `deploy/docker/` with an `env.template`, a chart and an entry in the README table. Add
  its versioned files to `release-please-config.json`.

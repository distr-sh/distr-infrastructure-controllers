# syntax=docker/dockerfile:1

# x-release-please-start-version
ARG CONTROLLER_IMAGE=ghcr.io/distr-sh/distr-infrastructure-controllers/opentofu-controller:0.2.0
# x-release-please-end

# The OpenTofu controller with the OpenTofu configuration of this example, which it copies into the data volume
# next to the state on every start.
FROM ${CONTROLLER_IMAGE}
ARG VERSION
ENV APP_VERSION=${VERSION}
COPY . /module/

# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG COMMIT

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -o opentofu-controller \
    -ldflags="-s -w -X github.com/distr-sh/distr-infrastructure-controllers/internal/buildconfig.version=${VERSION:-snapshot} -X github.com/distr-sh/distr-infrastructure-controllers/internal/buildconfig.commit=${COMMIT}" \
    ./cmd/controller/opentofu/

FROM ghcr.io/opentofu/opentofu:1.13.1-minimal AS tofu

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tini \
    && addgroup -g 65532 opentofu \
    && adduser -D -u 65532 -G opentofu -h /home/opentofu opentofu \
    && mkdir -p /module /data \
    && chown 65532:65532 /data
COPY --from=tofu /usr/local/bin/tofu /usr/local/bin/tofu
COPY --from=builder /workspace/opentofu-controller /usr/local/bin/opentofu-controller

# The image of an infrastructure application is built FROM this image and copies its OpenTofu configuration
# to MODULE_DIR. The controller copies it into DATA_DIR, the volume with the state, before it runs OpenTofu.
ENV TF_IN_AUTOMATION=1 \
    TF_INPUT=0 \
    CHECKPOINT_DISABLE=1 \
    MODULE_DIR=/module \
    DATA_DIR=/data
USER 65532:65532
WORKDIR /data
EXPOSE 8686
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
  CMD ["/usr/local/bin/opentofu-controller", "healthcheck"]
# tini reaps the provider plugins OpenTofu leaves behind when it is stopped.
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/opentofu-controller"]
CMD ["run"]

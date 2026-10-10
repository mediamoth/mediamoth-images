# syntax=docker/dockerfile:1.10

ARG DEBIAN_IMAGE=debian:bookworm-slim
ARG PYTHON_IMAGE=python:3.12-slim-bookworm
ARG GO_IMAGE=golang:1.26
ARG POSTGRES_IMAGE=postgres:18-alpine
FROM ${DEBIAN_IMAGE} AS media-runtime

ARG GRPC_HEALTH_PROBE_VERSION=v0.4.57
ARG GRPC_HEALTH_PROBE_SHA256=9193773627d3ed5a0fabe37ed81264b4a8e9d5a2156ce09fb6010caaabb0f06b

LABEL org.opencontainers.image.source="https://github.com/mediamoth/mediamoth-images"

RUN apt-get update && apt-get install --yes --no-install-recommends \
      ca-certificates \
      ffmpeg \
      openssh-client \
      rsync \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ADD --chmod=755 --checksum=sha256:${GRPC_HEALTH_PROBE_SHA256} \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 \
    /grpc_health_probe

RUN ffmpeg -version >/dev/null && /grpc_health_probe -version >/dev/null

FROM ${PYTHON_IMAGE} AS python-source

FROM media-runtime AS chapter-ml

ARG CDML_REPOSITORY=https://github.com/andrew-avinante/commercial-detctor-ml.git
ARG CDML_REVISION=e9fba15fd4c267fa36269466d5e64c6c248d49c7
ARG TORCH_VERSION=2.14.0

LABEL org.opencontainers.image.title="MediaMoth chapter ML runtime" \
      org.opencontainers.image.description="Pinned CDML and PyTorch runtime for chapter-inserter"

# The official Python slim image and media-runtime both derive from Debian
# Bookworm. Copying /usr/local retains Python 3.12 while this image inherits
# the common MediaMoth runtime layer.
COPY --from=python-source /usr/local /usr/local

RUN --mount=type=cache,target=/root/.cache/pip \
    apt-get update && apt-get install --yes --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/* \
    && python -m pip install --no-cache-dir \
      "torch==${TORCH_VERSION}" \
      "git+${CDML_REPOSITORY}@${CDML_REVISION}" \
    && command -v cdml \
    && cdml --help >/dev/null \
    && python -c 'import torch; print(torch.__version__)'

WORKDIR /app

FROM media-runtime AS video-runtime

LABEL org.opencontainers.image.title="MediaMoth video runtime" \
      org.opencontainers.image.description="FFmpeg and HandBrake runtime for video-service"

RUN apt-get update && apt-get install --yes --no-install-recommends \
      handbrake-cli \
    && rm -rf /var/lib/apt/lists/* \
    && HandBrakeCLI --version >/dev/null

WORKDIR /app

FROM ${GO_IMAGE} AS postgres-build

WORKDIR /src
COPY postgres-init/go.mod postgres-init/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY postgres-init/main.go ./
COPY postgres-init/cmd ./cmd
COPY postgres-init/internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test ./... \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/postgres-init .

FROM ${POSTGRES_IMAGE} AS postgres-runtime

LABEL org.opencontainers.image.source="https://github.com/mediamoth/mediamoth-images" \
      org.opencontainers.image.title="MediaMoth PostgreSQL runtime" \
      org.opencontainers.image.description="PostgreSQL 18 with additive resource initialization on every startup"

COPY --from=postgres-build /out/postgres-init /usr/local/bin/postgres-init
COPY --chmod=755 scripts/postgres-entrypoint.sh /usr/local/bin/mediamoth-postgres-entrypoint.sh

ENTRYPOINT ["mediamoth-postgres-entrypoint.sh"]
CMD ["postgres"]

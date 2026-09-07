# syntax=docker/dockerfile:1.10

# Keep the source revision in versions.env and pass it with --build-arg. A
# service consumes a published digest of this image, never this mutable source.
ARG PYTHON_IMAGE=python:3.12-slim-bookworm
FROM ${PYTHON_IMAGE}

ARG CDML_REPOSITORY=https://github.com/andrew-avinante/commercial-detctor-ml.git
ARG CDML_REVISION=e9fba15fd4c267fa36269466d5e64c6c248d49c7

LABEL org.opencontainers.image.title="MediaMoth chapter ML runtime" \
      org.opencontainers.image.description="Pinned CDML and PyTorch runtime for chapter-inserter" \
      org.opencontainers.image.source="https://github.com/mediamoth/mediamoth-images"

# These are the runtime dependencies chapter-inserter previously installed in
# every image build. CDML resolves its own pinned ML dependencies, including
# PyTorch; the verification script proves the final runtime can import Torch.
RUN apt-get update && apt-get install --yes --no-install-recommends \
      ca-certificates \
      ffmpeg \
      git \
      openssh-client \
      rsync \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN --mount=type=cache,target=/root/.cache/pip \
    python -m pip install --no-cache-dir \
      "git+${CDML_REPOSITORY}@${CDML_REVISION}" \
    && command -v cdml \
    && python -c 'import torch; print(torch.__version__)'

WORKDIR /app

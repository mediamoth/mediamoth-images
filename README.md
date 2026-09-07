# MediaMoth Images

Reproducible base images shared by [MediaMoth](https://github.com/mediamoth/mediamoth)
services.

## Images

| Image | Purpose | Platforms |
| --- | --- | --- |
| `chapter-ml` | The CDML/PyTorch runtime used by chapter-inserter | `linux/amd64` |

`chapter-ml` is deliberately separate from ordinary service runtime images:
ML/CUDA dependencies are large, have a distinct patch cadence, and are only
needed by the chapter-inserter service.

## Build locally

Choose the registry namespace at build time. The image name remains stable when
moving from a private registry to GHCR.

```sh
export REGISTRY=registry.example.internal:5000
export TAG=dev

docker buildx build \
  --platform linux/amd64 \
  --tag "$REGISTRY/mediamoth/chapter-ml:$TAG" \
  --load \
  chapter-ml
```

For an HTTP-only registry, configure Docker and BuildKit to treat that registry
as HTTP before pulling or pushing. Do not expose an HTTP registry beyond the
trusted runner network.

## Consume from a service

Release consumers must use a digest, not a mutable tag:

```dockerfile
ARG MEDIAMOTH_IMAGE_REGISTRY=ghcr.io
FROM ${MEDIAMOTH_IMAGE_REGISTRY}/mediamoth/chapter-ml@sha256:<published-digest>
```

The MediaMoth release workflow supplies `MEDIAMOTH_IMAGE_REGISTRY`. Changing
that value moves a service from the local registry to GHCR without changing its
Dockerfile structure.

## Release process

1. Change an image Dockerfile or `versions.env`.
2. Open a pull request; CI builds the affected image and verifies its tools.
3. Merge to `main` and dispatch **Publish image** with an immutable version.
4. Record the emitted digest in the consuming service Dockerfile.

The publish workflow creates an SBOM and provenance attestation, scans the
image, and signs its digest. It defaults to GHCR, but accepts a host[:port]
registry and optional credentials for the current self-hosted registry.

## Maintenance

- Rebuild images on a schedule for Debian security fixes.
- Keep CDML, PyTorch, CUDA, and their compatibility policy in `versions.env`.
- Treat a GPU/CUDA update as a new image version and validate it on a runner
  with the target NVIDIA driver.
- Do not add generic service dependencies to `chapter-ml`; create a separate
  image once a dependency is genuinely shared.

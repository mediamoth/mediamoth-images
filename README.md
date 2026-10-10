# MediaMoth Images

Reproducible base images shared by [MediaMoth](https://github.com/mediamoth/mediamoth)
services.

## Images

| Image | Purpose | Platforms |
| --- | --- | --- |
| `media-runtime` | FFmpeg, rsync, OpenSSH, CA certificates, and gRPC health probe | `linux/amd64` |
| `chapter-ml` | `media-runtime` plus Python, pinned CDML, and PyTorch/CUDA | `linux/amd64` |
| `video-runtime` | `media-runtime` plus HandBrake | `linux/amd64` |
| `postgres-runtime` | PostgreSQL 18 with resource initialization on every startup | `linux/amd64` |

The Dockerfile has one target per image. `chapter-ml` and `video-runtime`
inherit the same `media-runtime` stage, so their published images share layers
while their service-specific dependencies remain isolated.

## Build locally

Choose the registry namespace at build time. The image name remains stable when
moving from a private registry to GHCR.

```sh
export REGISTRY=registry.example.internal:5000
export TAG=dev
export IMAGE=video-runtime

docker buildx build \
  --platform linux/amd64 \
  --target "$IMAGE" \
  --tag "$REGISTRY/mediamoth/$IMAGE:$TAG" \
  --push \
  .
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
2. Open a pull request; CI builds the affected image and verifies its tools
   during the build, without importing a second copy into the Docker daemon.
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

## PostgreSQL runtime

`postgres-runtime` creates missing users, databases, and schemas and applies
database and schema grants on **every container startup**, including with an
existing data volume. Initialization finishes before PostgreSQL starts accepting
network connections. Errors fail startup. Existing passwords, ownership, and extra
resources are preserved; removing configuration does not remove resources or
revoke permissions. Database grants support `CREATE`, `CONNECT`, `TEMPORARY`,
and `ALL` (all database privileges). Schema grants support `USAGE`, `CREATE`,
and `ALL` (all schema privileges). Grants do not give access to tables,
sequences, or future objects.

Build and run with a mounted configuration:

```sh
docker build --target postgres-runtime -t mediamoth/postgres-runtime:dev .

docker run --name mediamoth-postgres \
  -e POSTGRES_PASSWORD=your-admin-password \
  -e POSTGRESUSERS__0__PASSWORD=your-app-password \
  -v "$PWD/examples/postgres.yaml:/etc/mediamoth/postgres.yaml:ro" \
  -v mediamoth-postgres-data:/var/lib/postgresql \
  -p 5432:5432 \
  mediamoth/postgres-runtime:dev
```

For PostgreSQL 18, mount the volume at `/var/lib/postgresql`. The image preserves
the official bootstrap settings, `POSTGRES_PASSWORD_FILE`, first-run scripts in
`/docker-entrypoint-initdb.d`, and PostgreSQL command-line arguments. Set
`POSTGRES_INIT_CONFIG` to change the mounted YAML path. The bootstrap user needs
to match the existing cluster when reusing a volume.

Configuration follows this shape (see [examples/postgres.yaml](examples/postgres.yaml)):

```yaml
PostgresUsers:
  - Username: media_app
    Password: replace-this-password
PostgresDatabases:
  - Name: media
    Grant:
      - Username: media_app
        Perms: [CREATE]
    PostgresSchemas:
      - Name: application
        Grant:
          - Username: media_app
            Perms: [USAGE, CREATE]
```

The database-level `Grant` list is optional; existing configurations continue
to work. Database `CREATE` lets migration tools run `CREATE SCHEMA IF NOT EXISTS`,
including when a schema already exists. Schema `CREATE` allows creating objects
inside that schema. Database grants are applied after users and databases exist,
before schema initialization, and are reapplied on every startup. PostgreSQL's
existing default privileges are preserved.

Every grant username must appear in `PostgresUsers`. Names must be unique in
their scope, valid UTF-8, and at most 63 bytes; user passwords must be nonempty.
Unknown fields and invalid grants fail validation before provisioning changes.
New users receive SCRAM-SHA-256 password verifiers, keeping plaintext passwords
out of PostgreSQL DDL statements and their logs.
You can use empty lists to configure only users or only databases.

Environment overrides follow the tagged YAML paths with `__` separators and
zero-based list indices:

```sh
POSTGRESUSERS__0__PASSWORD=app-password
POSTGRESDATABASES__0__NAME=media
POSTGRESDATABASES__0__GRANT__0__PERMS__0=CREATE
POSTGRESDATABASES__0__POSTGRESSCHEMAS__0__GRANT__0__PERMS__1=ALL
```

Overrides replace existing scalar entries. Define the list structure in YAML;
add users, databases, schemas, or grants in the file, then restart the container.
An out-of-range index, unknown field, or empty password override fails validation.
Changing a configured password affects creation of a missing user only.

The Go executable also works against a running PostgreSQL server:

```sh
cd postgres-init
go run . init --config ../examples/postgres.yaml
```

For standalone use, add an optional connection section:

```yaml
PostgresConnection:
  Host: localhost
  Port: 5432
  Username: postgres
  Password: your-admin-password
  Database: postgres
  SSLMode: disable
```

Its defaults are `/var/run/postgresql`, port `5432`, username/database `postgres`,
an empty password, and SSL mode `disable`. Connection fields also support env
overrides, e.g. `POSTGRESCONNECTION__PASSWORD`. During image startup, the wrapper
sets connection overrides to this container's local socket and bootstrap
credentials so provisioning runs against the local cluster.

To verify changes:

```sh
cd postgres-init
go test ./...
go vet ./...
# Include real catalog/SQL tests against a disposable database:
TEST_POSTGRES_DSN='postgres://postgres:password@localhost:5432/postgres?sslmode=disable' go test -race ./...
../scripts/test-postgres-image.sh mediamoth/postgres-runtime:dev
```

CI tests the Go code against PostgreSQL 18 and runs the image lifecycle test
covering initial provisioning, updated configuration on an existing volume,
restarts, bootstrap secrets, database and schema grants, and startup failures.

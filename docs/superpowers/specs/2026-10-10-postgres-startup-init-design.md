# PostgreSQL startup initializer

Build a PostgreSQL 18 Alpine image in this repository that ensures declared users, databases, schemas, and schema grants exist on every startup, including existing volumes. Keep provisioning additive and simple.

## Interface and configuration

The executable is `postgres-init`, with one command: `init --config /etc/mediamoth/postgres.yaml`. Cobra owns command wiring; Viper loads YAML and environment overrides. The layout is `cmd/root.go`, `cmd/init.go`, `internal/config/configuration.go`, and `internal/pgx5`. Use named primitive types for resource names, credentials, connection values, permissions, and paths.

YAML uses `PostgresUsers: [{Username, Password}]` and `PostgresDatabases: [{Name, PostgresSchemas: [{Name, Grant: [{Username, Perms}]}]}]`. `PostgresConnection` defines Host, Port, Username, Password, Database, and SSLMode. Standalone defaults connect through `/var/run/postgresql`, port 5432, user/database postgres, SSL disabled. Indexed overrides use uppercase tagged paths separated by `__`, e.g. `POSTGRESUSERS__0__PASSWORD`. Override existing YAML entries, including individual permission entries; do not silently append list items. Reject unknown YAML fields and malformed overrides of known roots.

Validate before database changes: valid nonempty names (UTF-8, no NUL, maximum 63 bytes), unique usernames/database names/schema names, nonempty passwords, grant usernames declared in PostgresUsers, nonempty grants containing only USAGE, CREATE, or ALL, and valid connection settings. Passwords never appear in logs or errors.

## Provisioning

Use pgx v5. Create missing login users, databases, and schemas in that order. Existing user passwords, role attributes, ownership, and extra resources remain unchanged. Reapply schema GRANT statements on every invocation; they are idempotent. ALL means all schema privileges only. No table/sequence privileges, default privileges, revocations, ownership changes, migration tracking, or password rotation. Quote SQL identifiers and send SCRAM-SHA-256 password verifiers generated with xdg-go/scram so PostgreSQL statement logs cannot expose plaintext application passwords.

## Image startup

Add a `postgres-runtime` target and Go build stage to the existing Dockerfile. Source official PostgreSQL entrypoint helpers, preserving bootstrap initialization and first-run init hooks. Start a socket-only temporary server using /var/run/postgresql for provisioning on every startup, then stop it and exec postgres in the foreground. Clean up the temporary server on failure or interruption. Connect locally with the bootstrap user's credentials, supporting POSTGRES_PASSWORD_FILE through the official helper. Pass through other commands and PostgreSQL help/version requests. Fail startup if configuration or provisioning fails.

Wire the target into existing CI/publishing workflows and versions.env; document build, configuration, overrides, volume mount, and additive behavior. Unit tests cover validation and indexed overrides. Integration tests cover actual PostgreSQL catalog changes, repeated initialization, adding resources to an existing volume, password preservation, SQL quoting, bootstrap credentials, and invalid-config startup failure.

## Authorization

The user approved the design and explicitly instructed implementation on `feature/postgres-startup-init`.

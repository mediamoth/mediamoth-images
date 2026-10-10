# PostgreSQL Startup Initializer Implementation Plan

> **For agentic workers:** Use executing-plans to implement task by task with test-driven-development.

**Goal:** Provision missing PostgreSQL resources on every image startup.
**Architecture:** Typed YAML configuration and Viper indexed env overrides feed a pgx v5 initializer. An entrypoint wrapper uses the official PostgreSQL bootstrap helpers and a temporary local server before execing postgres.
**Tech Stack:** Go, Cobra, Viper, pgx v5, PostgreSQL 18 Alpine.
**Spec:** docs/superpowers/specs/2026-10-10-postgres-startup-init-design.md

## Global Constraints

- Single `postgres-init init` command; schema permissions only: USAGE, CREATE, ALL.
- Additive creation; no password updates, ownership changes, revocations, or migrations.
- Named primitive types and struct tags define configuration.
- Existing YAML list entries have numeric `__` env overrides.
- Work in the user-requested branch; implementation is authorized without further approval.

## Review Focus

- Existing volumes and custom bootstrap credentials must initialize successfully.
- Quoted SQL identifiers and passwords must remain data.
- Invalid config and unknown indexed overrides must fail before mutation.
- Failed initialization must stop the temporary server and fail startup.
- Grants must never apply to tables, sequences, or future objects.

### Task 1: Configuration and loading

**Files:** go.mod, internal/config/configuration.go, internal/config/load.go, internal/config/configuration_test.go, internal/config/load_test.go.
**Interfaces:** `Load(path ConfigPath) (Configuration, error)` and `(Configuration).Validate() error`.
- [x] Write tests for valid configuration, unknown grant users, invalid permissions, duplicates, invalid names, empty passwords, unknown YAML fields, and indexed overrides.
- [x] Run `go test ./internal/config` and observe missing implementation.
- [x] Implement named types, validation, Viper loading, and indexed env binding.
- [x] Run `go test ./internal/config`; expected PASS.

### Task 2: CLI and provisioning

**Files:** main.go, cmd/root.go, cmd/init.go, internal/pgx5/init.go, internal/pgx5/password.go, internal/pgx5/init_test.go.
**Interfaces:** `pgx5.Init(ctx context.Context, cfg config.Configuration) error`, `cmd.Execute(ctx context.Context) error`.
- [x] Write PostgreSQL integration tests for repeated creation, additive resources, retained password and ownership, quoted identifiers/passwords, and schema-only grants.
- [x] Run tests against a dedicated disposable PostgreSQL server; observe missing implementation.
- [x] Implement catalog existence checks, quoted DDL, schema grants, and Cobra command wiring.
- [x] Run `go test ./...` against the disposable server; expected PASS.

### Task 3: Image lifecycle and repository integration

**Files:** Dockerfile, scripts/postgres-entrypoint.sh, scripts/test-postgres-image.sh, examples/postgres.yaml, README.md, versions.env, .github/workflows/ci.yaml, .github/workflows/publish.yaml, .dockerignore.
**Interfaces:** `postgres-runtime` Docker target; `scripts/test-postgres-image.sh IMAGE`.
- [x] Write image test for first boot, restart with added resources, invalid configuration failure, and alternate bootstrap user/password.
- [x] Run image test with stock postgres to observe absent provisioning.
- [x] Implement wrapper and Go build target; wire CI/publishing and documentation.
- [x] Build target and run image tests; expected PASS.
- [x] Run `go test -race ./...`, `go vet ./...`, `git diff --check`, and shell syntax checks; expected PASS.
- [x] Request an independent whole-change review and resolve material findings.

## Verified review fixes

YAML port overflow and fractional values, credential exposure in malformed YAML errors, and existing-volume startup with a custom socket directory each have a regression test observed RED then GREEN. PostgreSQL error statement logs were confirmed to expose plaintext user passwords; creation now sends SCRAM-SHA-256 verifiers. The image log regression fails on the original image and passes on the fixed image. Full race tests run in golang:1.26 because the host lacks a C compiler. Workflows pass actionlint; the image builds with the exact versions.env digests.

#!/usr/bin/env bash
set -Eeuo pipefail

image=${1:?usage: test-postgres-image.sh IMAGE}
fixture_dir=$(mktemp -d)
container="mediamoth-postgres-test-$$-$RANDOM"
volume="$container-data"
cleanup() {
  docker rm --force "$container" >/dev/null 2>&1 || true
  docker volume rm "$volume" >/dev/null 2>&1 || true
  rm -rf "$fixture_dir"
}
trap cleanup EXIT

# The postgres process must be able to read the bind-mounted config and secret.
chmod 755 "$fixture_dir"
cat > "$fixture_dir/postgres.yaml" <<'YAML'
PostgresUsers:
  - Username: app
    Password: initial-password
PostgresDatabases:
  - Name: media
    Grant:
      - Username: app
        Perms: [CREATE]
    PostgresSchemas:
      - Name: application
        Grant:
          - Username: app
            Perms: [USAGE, CREATE]
YAML
printf '%s' 'admin-test-password' > "$fixture_dir/admin-password"
printf '%s\n' 'CREATE TABLE bootstrap_hook (id integer);' > "$fixture_dir/hook.sql"
chmod 644 "$fixture_dir"/*
docker volume create "$volume" >/dev/null

start() {
  docker run --detach --name "$container" \
    --mount "type=volume,src=$volume,dst=/var/lib/postgresql" \
    --mount "type=bind,src=$fixture_dir,dst=/etc/mediamoth,readonly" \
    --mount "type=bind,src=$fixture_dir/hook.sql,dst=/docker-entrypoint-initdb.d/hook.sql,readonly" \
    --env POSTGRES_USER=bootstrap \
    --env POSTGRES_PASSWORD_FILE=/etc/mediamoth/admin-password \
    --env POSTGRES_INITDB_ARGS=--auth-local=scram-sha-256 \
    --env POSTGRESUSERS__0__PASSWORD=overridden-password \
    "$image" "$@" >/dev/null
}

ready() {
  for ((attempt=0; attempt<60; attempt++)); do
    if docker exec "$container" pg_isready -h 127.0.0.1 -U bootstrap >/dev/null 2>&1; then return; fi
    if [[ $(docker inspect --format '{{.State.Running}}' "$container") != true ]]; then
      docker logs "$container" >&2
      return 1
    fi
    sleep 1
  done
  docker logs "$container" >&2
  return 1
}

sql() {
  docker exec --env PGPASSWORD=admin-test-password "$container" \
    psql -h 127.0.0.1 -X -At --set ON_ERROR_STOP=1 --username bootstrap --dbname "${2:-bootstrap}" --command "$1"
}

assert_sql() {
  local expected=$1 query=$2 database=${3:-bootstrap} actual
  actual=$(sql "$query" "$database")
  if [[ $actual != "$expected" ]]; then
    printf 'Expected %s, got %s for %s\n' "$expected" "$actual" "$query" >&2
    return 1
  fi
}

start
ready
assert_sql t "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='app')"
assert_sql t "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='media')"
assert_sql t "SELECT has_database_privilege('app','media','CREATE')"
assert_sql t "SELECT has_schema_privilege('app','application','USAGE') AND has_schema_privilege('app','application','CREATE')" media
assert_sql t "SELECT to_regclass('bootstrap_hook') IS NOT NULL"
docker exec --env PGPASSWORD=overridden-password "$container" \
  psql -h 127.0.0.1 -X -At --set ON_ERROR_STOP=1 --username app --dbname media \
    --command 'CREATE SCHEMA IF NOT EXISTS application; CREATE SCHEMA migration_schema' >/dev/null
# A rejected role must not put the application's plaintext password in logs.
cat > "$fixture_dir/rejected-role.yaml" <<'YAML'
PostgresConnection:
  Host: 127.0.0.1
  Username: bootstrap
  Password: admin-test-password
  Database: bootstrap
PostgresUsers:
  - Username: pg_rejected_test
    Password: log-test-password
YAML
if docker exec --env POSTGRESUSERS__0__PASSWORD=log-test-password "$container" \
  postgres-init init --config /etc/mediamoth/rejected-role.yaml >/dev/null 2>&1; then
  printf 'Reserved role was unexpectedly created\n' >&2
  exit 1
fi
if docker logs "$container" 2>&1 | grep -q 'log-test-password'; then
  printf 'PostgreSQL logs exposed the plaintext password\n' >&2
  exit 1
fi
original_password=$(sql "SELECT rolpassword FROM pg_authid WHERE rolname='app'")
sql 'CREATE TABLE application.existing_table (id integer)' media >/dev/null
assert_sql f "SELECT has_table_privilege('app','application.existing_table','SELECT')" media
sql 'REVOKE CREATE ON DATABASE media FROM app' >/dev/null

docker stop --time 10 "$container" >/dev/null
docker rm "$container" >/dev/null
cat > "$fixture_dir/postgres.yaml" <<'YAML'
PostgresUsers:
  - Username: app
    Password: changed-password
  - Username: added-user
    Password: added-password
PostgresDatabases:
  - Name: media
    Grant:
      - Username: app
        Perms: [CREATE]
      - Username: added-user
        Perms: [CREATE]
    PostgresSchemas:
      - Name: application
        Grant:
          - Username: app
            Perms: [USAGE, CREATE]
      - Name: added-schema
        Grant:
          - Username: added-user
            Perms: [ALL]
  - Name: added-database
    Grant:
      - Username: added-user
        Perms: [ALL]
    PostgresSchemas: []
YAML
start postgres -c unix_socket_directories=/tmp
ready
assert_sql "$original_password" "SELECT rolpassword FROM pg_authid WHERE rolname='app'"
assert_sql t "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='added-user')"
assert_sql t "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='added-database')"
assert_sql t "SELECT has_database_privilege('app','media','CREATE') AND has_database_privilege('added-user','media','CREATE')"
assert_sql t "SELECT has_database_privilege('added-user','added-database','CREATE')"
assert_sql t "SELECT has_schema_privilege('added-user','added-schema','CREATE')" media
assert_sql f "SELECT has_table_privilege('app','application.existing_table','SELECT')" media
assert_sql t "SELECT to_regclass('bootstrap_hook') IS NOT NULL"

# Repeated startup must preserve resources and not rerun the first-boot SQL hook.
sql 'REVOKE CREATE ON DATABASE media FROM app' >/dev/null
docker restart --time 10 "$container" >/dev/null
ready
assert_sql t "SELECT has_database_privilege('app','media','CREATE')"
assert_sql t "SELECT has_schema_privilege('added-user','added-schema','USAGE')" media

docker stop --time 10 "$container" >/dev/null
docker rm "$container" >/dev/null
cat > "$fixture_dir/postgres.yaml" <<'YAML'
PostgresUsers:
  - Username: should-not-exist
    Password: secret
PostgresDatabases:
  - Name: media
    PostgresSchemas:
      - Name: application
        Grant:
          - Username: missing-user
            Perms: [USAGE]
YAML
start
exit_code=$(timeout 30 docker wait "$container")
if [[ $exit_code == 0 ]]; then
  printf 'Invalid configuration did not fail startup\n' >&2
  exit 1
fi
# Cleanup on failure must stop the temporary server cleanly.
if ! docker logs "$container" 2>&1 | grep -q 'server stopped'; then
  docker logs "$container" >&2
  exit 1
fi

docker run --rm "$image" postgres --version >/dev/null
docker run --rm "$image" postgres-init init --help >/dev/null
printf 'PostgreSQL image lifecycle tests passed.\n'

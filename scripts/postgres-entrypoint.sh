#!/usr/bin/env bash
set -Eeuo pipefail

# Reuse the official image's bootstrap, secret, privilege, and init-hook helpers.
source /usr/local/bin/docker-entrypoint.sh

if [[ ${1:-} == -* ]]; then
  set -- postgres "$@"
fi
if [[ ${1:-} != postgres ]] || _pg_want_help "$@"; then
  exec /usr/local/bin/docker-entrypoint.sh "$@"
fi

docker_setup_env
docker_create_db_directories
if [[ $(id -u) == 0 ]]; then
  exec gosu postgres "$BASH_SOURCE" "$@"
fi

fresh=false
if [[ -z $DATABASE_ALREADY_EXISTS ]]; then
  docker_verify_minimum_env
  docker_error_old_databases
  ls /docker-entrypoint-initdb.d/ >/dev/null
  docker_init_database_dir
  pg_setup_hba_conf "$@"
  fresh=true
fi

export PGPASSWORD="${PGPASSWORD:-$POSTGRES_PASSWORD}"
temporary_server=false
initializer_pid=''
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [[ -n $initializer_pid ]]; then
    kill -TERM "$initializer_pid" 2>/dev/null || true
    wait "$initializer_pid" 2>/dev/null || true
  fi
  if [[ $temporary_server == true && -s $PGDATA/postmaster.pid ]]; then
    docker_temp_server_stop || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

temporary_server=true
docker_temp_server_start "$@" -c unix_socket_directories=/var/run/postgresql
if [[ $fresh == true ]]; then
  docker_setup_db
  docker_process_init_files /docker-entrypoint-initdb.d/*
fi

# The startup initializer always connects to this container's local cluster.
# Standalone postgres-init invocations use the YAML connection settings.
export POSTGRESCONNECTION__HOST=/var/run/postgresql
export POSTGRESCONNECTION__PORT="${PGPORT:-5432}"
export POSTGRESCONNECTION__USERNAME="$POSTGRES_USER"
export POSTGRESCONNECTION__PASSWORD="$PGPASSWORD"
export POSTGRESCONNECTION__DATABASE="$POSTGRES_DB"
export POSTGRESCONNECTION__SSLMODE=disable
postgres-init init --config "${POSTGRES_INIT_CONFIG:-/etc/mediamoth/postgres.yaml}" &
initializer_pid=$!
wait "$initializer_pid"
initializer_pid=''
docker_temp_server_stop
temporary_server=false
trap - EXIT INT TERM
unset PGPASSWORD
unset "${!POSTGRES_@}"
unset "${!POSTGRESCONNECTION__@}"
exec "$@"

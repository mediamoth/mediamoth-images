#!/usr/bin/env sh
set -eu

minimum_gib=${1:-15}
available=$(df --output=avail -B1 / | tail -n 1)
required=$((minimum_gib * 1024 * 1024 * 1024))

docker system df || true

if [ "$available" -lt "$required" ]; then
  echo "chapter-ml requires at least ${minimum_gib} GiB free on /; found ${available} bytes." >&2
  echo 'Free runner disk space before retrying; do not prune Docker while another job is running.' >&2
  exit 1
fi

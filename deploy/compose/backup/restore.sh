#!/usr/bin/env bash
# Restore Postgres from a pg_dump custom-format file created by backup.sh.
# Usage: ./restore.sh /path/to/pca-pg-YYYYMMDD.dump
#
# WARNING: drops and recreates the public schema in the target DB.
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <pca-pg-*.dump>" >&2
  exit 1
fi

DUMP="$1"
if [[ ! -f "$DUMP" ]]; then
  echo "file not found: $DUMP" >&2
  exit 1
fi

COMPOSE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -f "$COMPOSE_DIR/.env" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$COMPOSE_DIR/.env" && set +a
fi

POSTGRES_USER="${POSTGRES_USER:-app}"
POSTGRES_DB="${POSTGRES_DB:-app}"

echo "This will REPLACE all data in database '$POSTGRES_DB'."
read -r -p "Type RESTORE to continue: " confirm
if [[ "$confirm" != "RESTORE" ]]; then
  echo "aborted"
  exit 1
fi

echo "Stopping server to release connections..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" stop server || true

echo "Restoring from $DUMP ..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" exec -T postgres \
  pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner --no-acl \
  <"$DUMP"

echo "Restarting stack..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d postgres redis server

echo "restore complete — run ./test-e2e.sh smoke or curl /healthz"

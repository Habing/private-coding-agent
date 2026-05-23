#!/usr/bin/env bash
# Daily Postgres backup for compose pilot deployments.
# Usage: ./backup.sh   (from deploy/compose/backup or any cwd)
set -euo pipefail

COMPOSE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-$COMPOSE_DIR/backups}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

if [[ -f "$COMPOSE_DIR/.env" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$COMPOSE_DIR/.env" && set +a
fi

POSTGRES_USER="${POSTGRES_USER:-app}"
POSTGRES_DB="${POSTGRES_DB:-app}"

mkdir -p "$BACKUP_DIR"
OUT="$BACKUP_DIR/pca-pg-${STAMP}.dump"

echo "Backing up Postgres ($POSTGRES_DB) → $OUT"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" exec -T postgres \
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl \
  >"$OUT"

# Optional MinIO mirror when mc is installed and compose minio is up.
if command -v mc >/dev/null 2>&1; then
  MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}"
  MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin}"
  MINIO_BUCKET="${PCA_SNAPSHOT_BUCKET:-pca-snapshots}"
  MINIO_DIR="$BACKUP_DIR/minio-${STAMP}"
  if mc alias set pca-local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null 2>&1; then
    mkdir -p "$MINIO_DIR"
    mc mirror --quiet "pca-local/${MINIO_BUCKET}" "$MINIO_DIR" || echo "warn: minio mirror skipped (bucket empty or unreachable)"
  fi
fi

echo "done: $OUT ($(du -h "$OUT" | cut -f1))"

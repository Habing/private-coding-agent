#!/usr/bin/env bash
# One-shot production pilot for compose (backup / restore / re-embed).
set -euo pipefail

JQ_IMG=ghcr.io/jqlang/jq:1.7.1
if ! docker image inspect "$JQ_IMG" >/dev/null 2>&1; then
  docker pull -q "$JQ_IMG" >/dev/null 2>&1 || JQ_IMG=stedolan/jq:latest
fi
jq() { docker run --rm -i "$JQ_IMG" "$@"; }

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
COMPOSE="$ROOT/deploy/compose"

cd "$COMPOSE"
docker compose up -d --build >/dev/null
sleep 15
curl -fsS http://localhost:8080/healthz
echo

echo "=== bootstrap demo user ==="
HASH='$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
docker compose exec -T postgres psql -U app -d app -v ON_ERROR_STOP=1 <<SQL >/dev/null
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'demo@example.com', '$HASH', 'Demo', 'admin')
ON CONFLICT (tenant_id, email) DO NOTHING;
SQL

docker compose exec -T redis redis-cli CONFIG GET appendonly

echo "=== backup syntax + initial dump ==="
cd "$COMPOSE/backup"
bash -n backup.sh restore.sh
./backup.sh
DUMP=$(ls -t ../backups/pca-pg-*.dump | head -1)
test -s "$DUMP"
echo "dump ok: $DUMP ($(du -h "$DUMP" | cut -f1))"

echo "=== DR restore round-trip ==="
TOK=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"knowledge","content":"pilot-dr-marker-pre","tags":["pilot-dr"]}' >/dev/null

./backup.sh
DUMP=$(ls -t ../backups/pca-pg-*.dump | head -1)

curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"knowledge","content":"pilot-dr-marker-post","tags":["pilot-dr"]}' >/dev/null

echo RESTORE | ./restore.sh "$DUMP"
sleep 15
curl -fsS http://localhost:8080/healthz
echo

TOK=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

MEM=$(curl -fsS "http://localhost:8080/memories?tag=pilot-dr" \
  -H "Authorization: Bearer $TOK")
PRE=$(echo "$MEM" | jq '[.memories[]? | select(.content=="pilot-dr-marker-pre")] | length')
POST=$(echo "$MEM" | jq '[.memories[]? | select(.content=="pilot-dr-marker-post")] | length')
echo "restore check: pre=$PRE post=$POST"
[[ "$PRE" == "1" && "$POST" == "0" ]]

echo "=== re-embed SOP ==="
RE=$(curl -fsS -X POST http://localhost:8080/admin/memories/re-embed \
  -H "Authorization: Bearer $TOK")
echo "$RE" | jq .
UPD=$(echo "$RE" | jq -r .updated)
[[ "$UPD" -ge 1 ]]

AUD=$(curl -fsS "http://localhost:8080/audit?action=memory.reembed.complete&limit=5" \
  -H "Authorization: Bearer $TOK" | jq -r '.entries[0].action')
echo "audit=$AUD"
[[ "$AUD" == "memory.reembed.complete" ]]

echo "PILOT PASS"

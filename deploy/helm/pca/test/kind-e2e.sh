#!/usr/bin/env bash
# Kind e2e — exercises the K8s sandbox driver end-to-end against a real kind
# cluster. Independent of compose ./test-e2e.sh (which still validates the
# docker driver). Six steps:
#   1) Bootstrap the demo user directly via psql exec into the PG Pod.
#   2) Port-forward the server Service to localhost:18080.
#   3) Login + create sandbox session; verify a Pod landed in pca-sandboxes.
#   4) Write a file + exec read-back through SPDY exec.
#   5) NetworkPolicy=internal must block outbound curl to public Internet.
#   6) Destroy session + verify subsequent exec returns 404.

set -euo pipefail

NS=${PCA_NS:-pca-system}
SBNS=${PCA_SBNS:-pca-sandboxes}
PF_PORT=${PCA_PF_PORT:-18080}
BASE="http://localhost:${PF_PORT}"

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
info()  { printf '\033[36m%s\033[0m\n' "$*"; }

PF_PID=""
cleanup() {
  if [[ -n "$PF_PID" ]]; then
    kill "$PF_PID" 2>/dev/null || true
    wait "$PF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

info "[kind 1/6] bootstrap demo user via psql exec into pca-postgres ..."
PG_POD=$(kubectl -n "$NS" get pods -l app.kubernetes.io/component=postgres \
  -o jsonpath='{.items[0].metadata.name}')
if [[ -z "$PG_POD" ]]; then
  red "no postgres pod found in $NS"; exit 1
fi
# bcrypt hash of "demo123" — same value the compose e2e uses.
HASH='$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
kubectl -n "$NS" exec "$PG_POD" -- env PGPASSWORD=changeme \
  psql -U app -d app -v ON_ERROR_STOP=1 <<SQL
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES (
  (SELECT id FROM tenants WHERE slug='default'),
  'demo@example.com',
  '${HASH}',
  'Demo',
  'admin'
) ON CONFLICT (tenant_id, email) DO NOTHING;
SQL

info "[kind 2/6] port-forward svc/pca-server :$PF_PORT ..."
kubectl -n "$NS" port-forward svc/pca-server "${PF_PORT}:8080" >/dev/null 2>&1 &
PF_PID=$!
# Wait for /healthz to respond
for i in $(seq 1 30); do
  if curl -fsS "${BASE}/healthz" >/dev/null 2>&1; then break; fi
  sleep 1
done
if ! curl -fsS "${BASE}/healthz" >/dev/null 2>&1; then
  red "port-forward never came up"; exit 1
fi

info "[kind 3/6] login + create sandbox ..."
TOK=$(curl -fsS -X POST "${BASE}/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' \
  | jq -r .token)
if [[ -z "$TOK" || "$TOK" == "null" ]]; then
  red "login failed"; exit 1
fi
SB=$(curl -fsS -X POST "${BASE}/sandbox/sessions" \
  -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{}')
ID=$(echo "$SB" | jq -r .id)
STATUS=$(echo "$SB" | jq -r .status)
if [[ "$STATUS" != "running" ]]; then
  red "expected status=running, got $STATUS"; echo "$SB"; exit 1
fi
# Sandbox Pod must materialize in the sandbox namespace.
POD=$(kubectl -n "$SBNS" get pods --no-headers \
  -o custom-columns=:metadata.name 2>/dev/null | head -n1)
if [[ -z "$POD" ]]; then
  red "no pod in $SBNS"; kubectl -n "$SBNS" get pods -o wide; exit 1
fi
green "  sandbox pod: $POD"

info "[kind 4/6] write + exec via SPDY ..."
B64=$(printf 'hello kind' | base64 | tr -d '\n')
curl -fsS -X PUT "${BASE}/sandbox/sessions/${ID}/files?path=hello.txt" \
  -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d "{\"content_base64\":\"$B64\"}" >/dev/null
EXEC=$(curl -fsS -X POST "${BASE}/sandbox/sessions/${ID}/exec" \
  -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"cmd":["cat","/workspace/hello.txt"]}')
OUT=$(echo "$EXEC" | jq -r .stdout_base64 | base64 -d)
if [[ "$OUT" != "hello kind" ]]; then
  red "stdout mismatch: got '$OUT'"; echo "$EXEC"; exit 1
fi

info "[kind 5/6] NetworkPolicy=internal must block external egress ..."
NPEXEC=$(curl -fsS -X POST "${BASE}/sandbox/sessions/${ID}/exec" \
  -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"cmd":["sh","-c","curl -m 4 -fsS https://1.1.1.1 >/dev/null 2>&1; echo $?"]}')
NPEXIT=$(echo "$NPEXEC" | jq -r .stdout_base64 | base64 -d | tr -d '[:space:]')
if [[ "$NPEXIT" == "0" ]]; then
  red "NetworkPolicy=internal failed to block egress (curl exit=0)"; exit 1
fi
green "  egress to 1.1.1.1 blocked (curl exit=$NPEXIT)"

info "[kind 6/6] destroy + verify 404 on subsequent exec ..."
curl -fsS -X DELETE "${BASE}/sandbox/sessions/${ID}" \
  -H "Authorization: Bearer $TOK" >/dev/null
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST "${BASE}/sandbox/sessions/${ID}/exec" \
  -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"cmd":["true"]}')
if [[ "$HTTP_CODE" != "404" ]]; then
  red "expected 404 after destroy, got $HTTP_CODE"; exit 1
fi

green "kind e2e PASS"

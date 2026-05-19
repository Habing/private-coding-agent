#!/usr/bin/env bash
# Slice 2 端到端验证 (跨平台 bash 版)。
# 前置:
#   - Docker Desktop 在跑
#   - pca/sandbox:base 镜像已 build (docker build -t pca/sandbox:base ../../sandbox/image)
#   - 当前目录 deploy/compose/, .env 已从 .env.example 复制 (脚本会自动补)
#   - 可用工具: docker, curl, jq, base64
#
# 用法:
#   cd deploy/compose
#   ./test-e2e.sh
set -euo pipefail

if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "[setup] copied .env.example -> .env"
fi

cleanup() { docker compose down >/dev/null 2>&1 || true; }
trap cleanup EXIT

# jq 通过 docker 调用 (主机可能无 jq)
JQ_IMG=ghcr.io/jqlang/jq:1.7.1
docker pull -q "$JQ_IMG" >/dev/null 2>&1 || JQ_IMG=stedolan/jq:latest
jq() { docker run --rm -i "$JQ_IMG" "$@"; }

echo "[1/8] starting compose ..."
docker compose up -d --build >/dev/null
sleep 20

echo "[2/8] inserting demo user via psql ..."
HASH='$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
docker compose exec -T postgres psql -U app -d app -v ON_ERROR_STOP=1 <<SQL >/dev/null
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'demo@example.com', '$HASH', 'Demo', 'admin')
ON CONFLICT (tenant_id, email) DO NOTHING;
SQL

echo "[3/8] login ..."
LOGIN=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}')
TOK=$(echo "$LOGIN" | jq -r .token)
[[ -n "$TOK" && "$TOK" != "null" ]] || { echo "login failed: $LOGIN"; exit 1; }

echo "[4/8] create sandbox ..."
SB=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
ID=$(echo "$SB" | jq -r .id)
STATUS=$(echo "$SB" | jq -r .status)
echo "  -> sandbox $ID, status=$STATUS"
[[ "$STATUS" == "running" ]] || { echo "expected running, got $STATUS"; exit 1; }

echo "[5/8] write file ..."
CONTENT=$(printf "hello world from e2e" | base64 -w0 2>/dev/null || printf "hello world from e2e" | base64)
curl -fsS -X PUT "http://localhost:8080/sandbox/sessions/$ID/files?path=hello.txt" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"content_base64\":\"$CONTENT\"}" >/dev/null

echo "[6/8] exec cat ..."
EXEC=$(curl -fsS -X POST "http://localhost:8080/sandbox/sessions/$ID/exec" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"cmd":["cat","/workspace/hello.txt"]}')
EXIT=$(echo "$EXEC" | jq -r .exit_code)
OUT=$(echo "$EXEC" | jq -r .stdout_base64 | base64 -d)
echo "  -> stdout: $OUT (exit=$EXIT)"
[[ "$OUT" == "hello world from e2e" ]] || { echo "stdout mismatch: $OUT"; exit 1; }

echo "[7/8] destroy ..."
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$ID" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[8/8] verify 404 after destroy ..."
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  "http://localhost:8080/sandbox/sessions/$ID/exec" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"cmd":["true"]}')
[[ "$HTTP_CODE" == "404" ]] || { echo "expected 404 got $HTTP_CODE"; exit 1; }

echo
echo "E2E PASS"

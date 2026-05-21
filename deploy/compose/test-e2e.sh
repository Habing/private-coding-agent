#!/usr/bin/env bash
# Slice 2 + Slice 3 端到端验证 (跨平台 bash 版)。
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
if ! docker image inspect "$JQ_IMG" >/dev/null 2>&1; then
  docker pull -q "$JQ_IMG" >/dev/null 2>&1 || JQ_IMG=stedolan/jq:latest
  if ! docker image inspect "$JQ_IMG" >/dev/null 2>&1; then
    docker pull -q "$JQ_IMG" >/dev/null 2>&1 || true
  fi
fi
jq() { docker run --rm -i "$JQ_IMG" "$@"; }

echo "[1/35] starting compose ..."
docker compose up -d --build >/dev/null
sleep 20

echo "[2/35] inserting demo user via psql ..."
HASH='$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
docker compose exec -T postgres psql -U app -d app -v ON_ERROR_STOP=1 <<SQL >/dev/null
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'demo@example.com', '$HASH', 'Demo', 'admin')
ON CONFLICT (tenant_id, email) DO NOTHING;
SQL

echo "[3/35] login ..."
LOGIN=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}')
TOK=$(echo "$LOGIN" | jq -r .token)
[[ -n "$TOK" && "$TOK" != "null" ]] || { echo "login failed: $LOGIN"; exit 1; }

echo "[4/35] create sandbox ..."
SB=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
ID=$(echo "$SB" | jq -r .id)
STATUS=$(echo "$SB" | jq -r .status)
echo "  -> sandbox $ID, status=$STATUS"
[[ "$STATUS" == "running" ]] || { echo "expected running, got $STATUS"; exit 1; }

echo "[5/35] write file ..."
CONTENT=$(printf "hello world from e2e" | base64 -w0 2>/dev/null || printf "hello world from e2e" | base64)
curl -fsS -X PUT "http://localhost:8080/sandbox/sessions/$ID/files?path=hello.txt" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"content_base64\":\"$CONTENT\"}" >/dev/null

echo "[6/35] exec cat ..."
EXEC=$(curl -fsS -X POST "http://localhost:8080/sandbox/sessions/$ID/exec" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"cmd":["cat","/workspace/hello.txt"]}')
EXIT=$(echo "$EXEC" | jq -r .exit_code)
OUT=$(echo "$EXEC" | jq -r .stdout_base64 | base64 -d)
echo "  -> stdout: $OUT (exit=$EXIT)"
[[ "$OUT" == "hello world from e2e" ]] || { echo "stdout mismatch: $OUT"; exit 1; }

echo "[7/35] destroy ..."
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$ID" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[8/35] verify 404 after destroy ..."
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  "http://localhost:8080/sandbox/sessions/$ID/exec" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"cmd":["true"]}')
[[ "$HTTP_CODE" == "404" ]] || { echo "expected 404 got $HTTP_CODE"; exit 1; }

echo "[9/35] chat completion (non-stream) via mock-provider ..."
CHAT=$(curl -fsS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}')
TEXT=$(echo "$CHAT" | jq -r '.choices[0].message.content')
[[ "$TEXT" == "hello from mock" ]] || { echo "chat content mismatch: $TEXT"; exit 1; }

echo "[10/35] chat completion (stream) via mock-provider ..."
STREAM=$(curl -fsS -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}')
echo "$STREAM" | grep -q "data: \[DONE\]" || { echo "stream missing [DONE]"; exit 1; }
echo "$STREAM" | grep -q '"content":"hello "' || { echo "stream missing chunk"; exit 1; }

echo "[11/35] embeddings via mock-provider ..."
EMB=$(curl -fsS -X POST http://localhost:8080/v1/embeddings \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:text","input":["hi"]}')
LEN=$(echo "$EMB" | jq '.data[0].embedding | length')
[[ "$LEN" == "3" ]] || { echo "embedding length mismatch: $LEN"; exit 1; }

echo "[12/35] verify model_usage rows ..."
docker compose exec -T postgres psql -U app -d app -t -c \
  "SELECT count(*) FROM model_usage WHERE status='ok';" | grep -q "[1-9]" \
  || { echo "model_usage has no rows"; exit 1; }

echo "[13/35] list tools ..."
TOOLS=$(curl -fsS http://localhost:8080/tools -H "Authorization: Bearer $TOK")
NAMES=$(echo "$TOOLS" | jq -r '.tools[].name' | sort | tr '\n' ',')
[[ "$NAMES" == "fs.glob,fs.list,fs.read,fs.write,grep,llm.chat,llm.embed,memory.delete,memory.list,memory.save,memory.search,shell.exec," ]] \
  || { echo "tools list mismatch: $NAMES"; exit 1; }

echo "[14/35] fs.write + fs.read round-trip ..."
SB2=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
ID2=$(echo "$SB2" | jq -r .id)
WRITE=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"tool\":\"fs.write\",\"input\":{\"sandbox_id\":\"$ID2\",\"path\":\"a.txt\",\"content\":\"tool e2e\"}}")
BW=$(echo "$WRITE" | jq -r '.output.bytes_written')
[[ "$BW" == "8" ]] || { echo "bytes_written mismatch: $BW"; exit 1; }
READ=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"tool\":\"fs.read\",\"input\":{\"sandbox_id\":\"$ID2\",\"path\":\"a.txt\"}}")
CONTENT=$(echo "$READ" | jq -r '.output.content')
[[ "$CONTENT" == "tool e2e" ]] || { echo "fs.read content mismatch: $CONTENT"; exit 1; }

echo "[15/35] shell.exec ls ..."
SHOUT=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"tool\":\"shell.exec\",\"input\":{\"sandbox_id\":\"$ID2\",\"cmd\":[\"ls\",\"/workspace\"]}}")
echo "$SHOUT" | jq -r '.output.stdout' | grep -q "a.txt" || { echo "shell.exec stdout missing a.txt"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$ID2" -H "Authorization: Bearer $TOK" >/dev/null

echo "[16/35] llm.chat + tool_invocations ..."
CHATTOOL=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"llm.chat","input":{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}}')
TEXT2=$(echo "$CHATTOOL" | jq -r '.output.content')
[[ "$TEXT2" == "hello from mock" ]] || { echo "llm.chat content mismatch: $TEXT2"; exit 1; }
docker compose exec -T postgres psql -U app -d app -t -c \
  "SELECT count(*) FROM tool_invocations WHERE status='ok';" | grep -q "[1-9]" \
  || { echo "tool_invocations has no rows"; exit 1; }

echo "[17/35] agent.run direct final ..."
RUN=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","messages":[{"role":"user","content":"hi"}]}')
LAST_KIND=$(echo "$RUN" | jq -r '.events[-1].kind')
[[ "$LAST_KIND" == "final" ]] || { echo "expected final got $LAST_KIND"; echo "$RUN"; exit 1; }
LAST_TEXT=$(echo "$RUN" | jq -r '.events[-1].text')
[[ "$LAST_TEXT" == "hello from mock" ]] || { echo "final text mismatch: $LAST_TEXT"; exit 1; }

echo "[18/35] agent.run with tool_call chain ..."
SBA=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
IDA=$(echo "$SBA" | jq -r .id)
RUN2=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"model\":\"default-mock:gpt-4o\",\"profile\":\"coding\",\"messages\":[{\"role\":\"user\",\"content\":\"list workspace files for sandbox $IDA\"}]}")
KINDS=$(echo "$RUN2" | jq -r '.events[].kind' | tr '\n' ',')
echo "  -> events: $KINDS"
echo "$KINDS" | grep -q "tool_call," || { echo "no tool_call event"; echo "$RUN2"; exit 1; }
echo "$KINDS" | grep -q "tool_result," || { echo "no tool_result event"; echo "$RUN2"; exit 1; }
LAST2=$(echo "$RUN2" | jq -r '.events[-1].kind')
[[ "$LAST2" == "final" ]] || { echo "expected final got $LAST2"; echo "$RUN2"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$IDA" -H "Authorization: Bearer $TOK" >/dev/null

echo "[19/35] POST /sessions ..."
SESS=$(curl -fsS -X POST http://localhost:8080/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","title":"e2e"}')
SID=$(echo "$SESS" | jq -r .id)
[[ -n "$SID" && "$SID" != "null" ]] || { echo "session create failed: $SESS"; exit 1; }
echo "  -> session $SID"

echo "[20/35] GET /sessions + GET /sessions/:id/messages ..."
LIST=$(curl -fsS http://localhost:8080/sessions -H "Authorization: Bearer $TOK")
echo "$LIST" | jq -e --arg id "$SID" '.sessions[] | select(.id==$id)' >/dev/null \
  || { echo "session $SID not in list: $LIST"; exit 1; }

echo "[21/35] WS round-trip via docker websocat ..."
WS_IMG=solsson/websocat
docker pull -q "$WS_IMG" >/dev/null 2>&1 || true
# Use the compose network and reach the server by service name —
# `--network host` does not route to localhost:8080 on Docker Desktop.
WS_OUT=$(printf '%s\n' '{"type":"user_message","content":"hi"}' \
  | docker run --rm -i --network compose_default "$WS_IMG" \
    -H="Authorization: Bearer $TOK" \
    -n1 \
    -t "ws://server:8080/sessions/$SID/ws" 2>/dev/null \
  | head -n 5)
echo "$WS_OUT" | grep -q '"type":"event"' || { echo "ws missing event frame: $WS_OUT"; exit 1; }
MSGS=$(curl -fsS "http://localhost:8080/sessions/$SID/messages" -H "Authorization: Bearer $TOK")
echo "$MSGS" | jq -e '.messages | length >= 2' >/dev/null \
  || { echo "messages not persisted: $MSGS"; exit 1; }

echo "[22/35] POST /memories x2 (different types) ..."
MEM1=$(curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"preference","content":"user prefers Go","tags":["go","lang"]}')
MID1=$(echo "$MEM1" | jq -r .id)
[[ -n "$MID1" && "$MID1" != "null" ]] || { echo "memory create 1 failed: $MEM1"; exit 1; }
MEM2=$(curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"knowledge","content":"uses postgres 16","tags":["db","pg"]}')
MID2=$(echo "$MEM2" | jq -r .id)
[[ -n "$MID2" && "$MID2" != "null" ]] || { echo "memory create 2 failed: $MEM2"; exit 1; }

echo "[23/35] GET /memories?type=preference&tag=go filter ..."
LISTMEM=$(curl -fsS "http://localhost:8080/memories?type=preference&tag=go" \
  -H "Authorization: Bearer $TOK")
echo "$LISTMEM" | jq -e --arg id "$MID1" '.memories[] | select(.id==$id)' >/dev/null \
  || { echo "filtered list missing preference memory: $LISTMEM"; exit 1; }
COUNT_PREF=$(echo "$LISTMEM" | jq '.memories | length')
[[ "$COUNT_PREF" == "1" ]] || { echo "expected 1 preference, got $COUNT_PREF"; exit 1; }

echo "[24/35] memory.save via tool -> memory.search via tool round-trip ..."
SAVE=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.save","input":{"type":"lesson","content":"rate-limit external APIs","tags":["infra","rl"]}}')
MID3=$(echo "$SAVE" | jq -r '.output.id')
[[ -n "$MID3" && "$MID3" != "null" ]] || { echo "memory.save failed: $SAVE"; exit 1; }
SRCH=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.search","input":{"query":"rate-limit"}}')
echo "$SRCH" | jq -e --arg id "$MID3" '.output.items[] | select(.id==$id)' >/dev/null \
  || { echo "memory.search did not find saved entry: $SRCH"; exit 1; }

echo "[25/35] DELETE /memories/{id} -> GET 404 ..."
curl -fsS -X DELETE "http://localhost:8080/memories/$MID1" \
  -H "Authorization: Bearer $TOK" >/dev/null
GET_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  "http://localhost:8080/memories/$MID1" -H "Authorization: Bearer $TOK")
[[ "$GET_CODE" == "404" ]] || { echo "expected 404 after delete, got $GET_CODE"; exit 1; }

# ---- Slice 8: Web UI ----
echo "[26/35] GET / returns SPA shell html ..."
HTML=$(curl -fsS http://localhost:8080/)
echo "$HTML" | grep -q 'id="root"' || { echo "root html missing"; echo "$HTML" | head -5; exit 1; }
CTYPE=$(curl -sI http://localhost:8080/ | tr -d '\r' | awk '/^[Cc]ontent-[Tt]ype:/{print $2}')
[[ "$CTYPE" == text/html* ]] || { echo "ctype: $CTYPE"; exit 1; }

echo "[27/35] GET /login (SPA fallback) returns the same shell ..."
HTML2=$(curl -fsS http://localhost:8080/login)
echo "$HTML2" | grep -q 'id="root"' || { echo "spa fallback failed for /login"; exit 1; }

echo "[28/35] API not shadowed by SPA fallback: GET /sessions returns JSON ..."
# Use GET (not HEAD) — gin doesn't auto-register HEAD for GET routes, so HEAD
# falls through to NoRoute and would serve the SPA shell. The contract under
# test is "GET /sessions returns JSON", which is what real clients do.
CT=$(curl -s -D - -o /dev/null -H "Authorization: Bearer $TOK" http://localhost:8080/sessions \
  | tr -d '\r' | awk '/^[Cc]ontent-[Tt]ype:/{print $2}')
[[ "$CT" == application/json* ]] || { echo "API content-type: $CT"; exit 1; }

# ---- Slice 9: Audit ----
echo "[29/35] GET /audit (admin) returns the access log ..."
AUDIT=$(curl -fsS -H "Authorization: Bearer $TOK" "http://localhost:8080/audit?limit=50")
TOTAL=$(echo "$AUDIT" | jq -r '.total')
[[ "$TOTAL" -ge 10 ]] || { echo "expected >=10 audit rows, got $TOTAL"; echo "$AUDIT" | head -c 500; exit 1; }

echo "[30/35] GET /audit?action=auth.login finds login event ..."
LOGIN_HITS=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=auth.login&limit=10" | jq '.entries | length')
[[ "$LOGIN_HITS" -ge 1 ]] || { echo "expected >=1 auth.login entry"; exit 1; }

echo "[31/35] GET /audit?action=sandbox. finds sandbox lifecycle event ..."
SB_HITS=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=sandbox.&limit=10" | jq '.entries | length')
[[ "$SB_HITS" -ge 1 ]] || { echo "expected >=1 sandbox.* audit entry"; exit 1; }

echo "[32/35] member user gets 403 from /audit ..."
docker compose exec -T postgres psql -U app -d app -v ON_ERROR_STOP=1 <<SQL >/dev/null
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'member@example.com', '$HASH', 'Member', 'member')
ON CONFLICT (tenant_id, email) DO UPDATE SET role='member';
SQL
MTOK=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"member@example.com","password":"demo123"}' | jq -r .token)
CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $MTOK" http://localhost:8080/audit)
[[ "$CODE" == "403" ]] || { echo "expected 403 for member, got $CODE"; exit 1; }

echo "[33/35] GET /metrics with admin JWT returns pca_* metrics ..."
METRICS_ADMIN=$(curl -fsS -H "Authorization: Bearer $TOKEN" http://localhost:8080/metrics)
echo "$METRICS_ADMIN" | grep -q '^pca_http_requests_total' \
  || { echo "expected pca_http_requests_total in /metrics body"; exit 1; }

echo "[34/35] GET /metrics with static scrape token also works ..."
SCRAPE_TOKEN="${PCA_OBSERVABILITY_METRICS_TOKEN:-dev-scrape-token-change-me}"
CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $SCRAPE_TOKEN" http://localhost:8080/metrics)
[[ "$CODE" == "200" ]] || { echo "expected 200 with scrape token, got $CODE"; exit 1; }

echo "[35/35] GET /metrics without auth is rejected ..."
CODE=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/metrics)
[[ "$CODE" == "401" ]] || { echo "expected 401 without auth, got $CODE"; exit 1; }

echo
echo "E2E PASS"

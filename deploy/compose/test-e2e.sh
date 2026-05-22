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

echo "[1/60] starting compose ..."
# Step 43 (sandbox quota exceeded) requires cap=1. The .env may have a higher
# override for manual dev; force =1 here so this script is self-contained.
export PCA_QUOTA_SANDBOX_MAX_ACTIVE=1
docker compose up -d --build >/dev/null
sleep 20

echo "[2/60] inserting demo user via psql ..."
HASH='$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
docker compose exec -T postgres psql -U app -d app -v ON_ERROR_STOP=1 <<SQL >/dev/null
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'demo@example.com', '$HASH', 'Demo', 'admin')
ON CONFLICT (tenant_id, email) DO NOTHING;

-- Reset app-level state so the script is idempotent across reruns
-- (compose_pgdata persists by default). Memories/sessions accumulate
-- otherwise and would dedup against prior-run rows (step 39).
TRUNCATE memories, messages, sessions, sandbox_sessions, audit_log RESTART IDENTITY CASCADE;
SQL

echo "[3/60] login ..."
LOGIN=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}')
TOK=$(echo "$LOGIN" | jq -r .token)
[[ -n "$TOK" && "$TOK" != "null" ]] || { echo "login failed: $LOGIN"; exit 1; }

echo "[4/60] create sandbox ..."
SB=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
ID=$(echo "$SB" | jq -r .id)
STATUS=$(echo "$SB" | jq -r .status)
echo "  -> sandbox $ID, status=$STATUS"
[[ "$STATUS" == "running" ]] || { echo "expected running, got $STATUS"; exit 1; }

echo "[5/60] write file ..."
CONTENT=$(printf "hello world from e2e" | base64 -w0 2>/dev/null || printf "hello world from e2e" | base64)
curl -fsS -X PUT "http://localhost:8080/sandbox/sessions/$ID/files?path=hello.txt" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"content_base64\":\"$CONTENT\"}" >/dev/null

echo "[6/60] exec cat ..."
EXEC=$(curl -fsS -X POST "http://localhost:8080/sandbox/sessions/$ID/exec" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"cmd":["cat","/workspace/hello.txt"]}')
EXIT=$(echo "$EXEC" | jq -r .exit_code)
OUT=$(echo "$EXEC" | jq -r .stdout_base64 | base64 -d)
echo "  -> stdout: $OUT (exit=$EXIT)"
[[ "$OUT" == "hello world from e2e" ]] || { echo "stdout mismatch: $OUT"; exit 1; }

echo "[7/60] destroy ..."
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$ID" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[8/60] verify 404 after destroy ..."
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  "http://localhost:8080/sandbox/sessions/$ID/exec" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"cmd":["true"]}')
[[ "$HTTP_CODE" == "404" ]] || { echo "expected 404 got $HTTP_CODE"; exit 1; }

echo "[9/60] chat completion (non-stream) via mock-provider ..."
CHAT=$(curl -fsS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}')
TEXT=$(echo "$CHAT" | jq -r '.choices[0].message.content')
[[ "$TEXT" == "hello from mock" ]] || { echo "chat content mismatch: $TEXT"; exit 1; }

echo "[10/60] chat completion (stream) via mock-provider ..."
STREAM=$(curl -fsS -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}')
echo "$STREAM" | grep -q "data: \[DONE\]" || { echo "stream missing [DONE]"; exit 1; }
echo "$STREAM" | grep -q '"content":"hello "' || { echo "stream missing chunk"; exit 1; }

echo "[11/60] embeddings via mock-provider ..."
EMB=$(curl -fsS -X POST http://localhost:8080/v1/embeddings \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:text","input":["hi"]}')
LEN=$(echo "$EMB" | jq '.data[0].embedding | length')
[[ "$LEN" == "1536" ]] || { echo "embedding length mismatch: $LEN"; exit 1; }

echo "[12/60] verify model_usage rows ..."
docker compose exec -T postgres psql -U app -d app -t -c \
  "SELECT count(*) FROM model_usage WHERE status='ok';" | grep -q "[1-9]" \
  || { echo "model_usage has no rows"; exit 1; }

echo "[13/60] list tools ..."
TOOLS=$(curl -fsS http://localhost:8080/tools -H "Authorization: Bearer $TOK")
NAMES=$(echo "$TOOLS" | jq -r '.tools[].name' | sort | tr '\n' ',')
[[ "$NAMES" == "agent.delegate,fs.glob,fs.list,fs.read,fs.write,grep,llm.chat,llm.embed,memory.delete,memory.list,memory.save,memory.search,shell.exec,workflow.create,workflow.get,workflow.list,workflow.update," ]] \
  || { echo "tools list mismatch: $NAMES"; exit 1; }

echo "[14/60] fs.write + fs.read round-trip ..."
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

echo "[15/60] shell.exec ls ..."
SHOUT=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"tool\":\"shell.exec\",\"input\":{\"sandbox_id\":\"$ID2\",\"cmd\":[\"ls\",\"/workspace\"]}}")
echo "$SHOUT" | jq -r '.output.stdout' | grep -q "a.txt" || { echo "shell.exec stdout missing a.txt"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$ID2" -H "Authorization: Bearer $TOK" >/dev/null

echo "[16/60] llm.chat + tool_invocations ..."
CHATTOOL=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"llm.chat","input":{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}}')
TEXT2=$(echo "$CHATTOOL" | jq -r '.output.content')
[[ "$TEXT2" == "hello from mock" ]] || { echo "llm.chat content mismatch: $TEXT2"; exit 1; }
docker compose exec -T postgres psql -U app -d app -t -c \
  "SELECT count(*) FROM tool_invocations WHERE status='ok';" | grep -q "[1-9]" \
  || { echo "tool_invocations has no rows"; exit 1; }

echo "[17/60] agent.run direct final ..."
RUN=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","messages":[{"role":"user","content":"hi"}]}')
LAST_KIND=$(echo "$RUN" | jq -r '.events[-1].kind')
[[ "$LAST_KIND" == "final" ]] || { echo "expected final got $LAST_KIND"; echo "$RUN"; exit 1; }
LAST_TEXT=$(echo "$RUN" | jq -r '.events[-1].text')
[[ "$LAST_TEXT" == "hello from mock" ]] || { echo "final text mismatch: $LAST_TEXT"; exit 1; }

echo "[18/60] agent.run with tool_call chain ..."
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

echo "[19/60] POST /sessions ..."
SESS=$(curl -fsS -X POST http://localhost:8080/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","title":"e2e"}')
SID=$(echo "$SESS" | jq -r .id)
[[ -n "$SID" && "$SID" != "null" ]] || { echo "session create failed: $SESS"; exit 1; }
echo "  -> session $SID"

echo "[20/60] GET /sessions + GET /sessions/:id/messages ..."
LIST=$(curl -fsS http://localhost:8080/sessions -H "Authorization: Bearer $TOK")
echo "$LIST" | jq -e --arg id "$SID" '.sessions[] | select(.id==$id)' >/dev/null \
  || { echo "session $SID not in list: $LIST"; exit 1; }

echo "[21/60] WS round-trip via docker websocat ..."
WS_IMG=solsson/websocat
docker pull -q "$WS_IMG" >/dev/null 2>&1 || true
# Use the compose network and reach the server by service name —
# `--network host` does not route to localhost:8080 on Docker Desktop.
# -n1 (= -n + -1): send one message, then keep the connection open without
# sending a WS Close so the server's async SendMessage events can flow back.
# Without -n1, websocat closes immediately on stdin EOF and the server's
# writeJSON calls fail silently — context.WithoutCancel keeps the agent
# run going for DB persistence but the client sees zero event frames.
sleep 2
WS_OUT=$(printf '%s\n' '{"type":"user_message","content":"hi"}' \
  | docker run --rm -i --network compose_default "$WS_IMG" \
    -H="Authorization: Bearer $TOK" \
    -n1 \
    -t "ws://server:8080/sessions/$SID/ws" 2>&1 \
  | head -n 12 || true)
echo "$WS_OUT" | grep -q '"type":"event"' || { echo "ws missing event frame: $WS_OUT"; docker compose logs server --tail 40 2>/dev/null || true; exit 1; }
sleep 2
MSGS=$(curl -fsS "http://localhost:8080/sessions/$SID/messages" -H "Authorization: Bearer $TOK")
echo "$MSGS" | jq -e '.messages | length >= 2' >/dev/null \
  || { echo "messages not persisted: $MSGS"; exit 1; }

echo "[21b/60] DELETE /sessions/:id archives session and releases sandbox ..."
curl -fsS -X DELETE "http://localhost:8080/sessions/$SID" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[22/60] POST /memories x2 (different types) ..."
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

echo "[23/60] GET /memories?type=preference&tag=go filter ..."
LISTMEM=$(curl -fsS "http://localhost:8080/memories?type=preference&tag=go" \
  -H "Authorization: Bearer $TOK")
echo "$LISTMEM" | jq -e --arg id "$MID1" '.memories[] | select(.id==$id)' >/dev/null \
  || { echo "filtered list missing preference memory: $LISTMEM"; exit 1; }
COUNT_PREF=$(echo "$LISTMEM" | jq '.memories | length')
[[ "$COUNT_PREF" == "1" ]] || { echo "expected 1 preference, got $COUNT_PREF"; exit 1; }

echo "[24/60] memory.save via tool -> memory.search via tool round-trip ..."
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

echo "[25/60] DELETE /memories/{id} -> GET 404 ..."
curl -fsS -X DELETE "http://localhost:8080/memories/$MID1" \
  -H "Authorization: Bearer $TOK" >/dev/null
GET_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  "http://localhost:8080/memories/$MID1" -H "Authorization: Bearer $TOK")
[[ "$GET_CODE" == "404" ]] || { echo "expected 404 after delete, got $GET_CODE"; exit 1; }

# ---- Slice 8: Web UI ----
echo "[26/60] GET / returns SPA shell html ..."
HTML=$(curl -fsS http://localhost:8080/)
echo "$HTML" | grep -q 'id="root"' || { echo "root html missing"; echo "$HTML" | head -5; exit 1; }
CTYPE=$(curl -sI http://localhost:8080/ | tr -d '\r' | awk '/^[Cc]ontent-[Tt]ype:/{print $2}')
[[ "$CTYPE" == text/html* ]] || { echo "ctype: $CTYPE"; exit 1; }

echo "[27/60] GET /login (SPA fallback) returns the same shell ..."
HTML2=$(curl -fsS http://localhost:8080/login)
echo "$HTML2" | grep -q 'id="root"' || { echo "spa fallback failed for /login"; exit 1; }

echo "[28/60] API not shadowed by SPA fallback: GET /sessions returns JSON ..."
# Use GET (not HEAD) — gin doesn't auto-register HEAD for GET routes, so HEAD
# falls through to NoRoute and would serve the SPA shell. The contract under
# test is "GET /sessions returns JSON", which is what real clients do.
CT=$(curl -s -D - -o /dev/null -H "Authorization: Bearer $TOK" http://localhost:8080/sessions \
  | tr -d '\r' | awk '/^[Cc]ontent-[Tt]ype:/{print $2}')
[[ "$CT" == application/json* ]] || { echo "API content-type: $CT"; exit 1; }

# ---- Slice 9: Audit ----
echo "[29/60] GET /audit (admin) returns the access log ..."
AUDIT=$(curl -fsS -H "Authorization: Bearer $TOK" "http://localhost:8080/audit?limit=50")
TOTAL=$(echo "$AUDIT" | jq -r '.total')
[[ "$TOTAL" -ge 10 ]] || { echo "expected >=10 audit rows, got $TOTAL"; echo "$AUDIT" | head -c 500; exit 1; }

echo "[30/60] GET /audit?action=auth.login finds login event ..."
LOGIN_HITS=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=auth.login&limit=10" | jq '.entries | length')
[[ "$LOGIN_HITS" -ge 1 ]] || { echo "expected >=1 auth.login entry"; exit 1; }

echo "[31/60] GET /audit?action=sandbox. finds sandbox lifecycle event ..."
SB_HITS=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=sandbox.&limit=10" | jq '.entries | length')
[[ "$SB_HITS" -ge 1 ]] || { echo "expected >=1 sandbox.* audit entry"; exit 1; }

echo "[32/60] member user gets 403 from /audit ..."
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

echo "[33/60] GET /metrics with admin JWT returns pca_* metrics ..."
curl -fsS -H "Authorization: Bearer $TOK" http://localhost:8080/metrics > /tmp/pca-metrics.txt \
  || { echo "metrics curl failed (token might be unauthorized)"; exit 1; }
grep -q '^pca_http_requests_total' /tmp/pca-metrics.txt \
  || { echo "expected pca_http_requests_total in /metrics body (body in /tmp/pca-metrics.txt, size=$(wc -c </tmp/pca-metrics.txt))"; grep -E "^pca_|^# HELP pca_" /tmp/pca-metrics.txt | head -10 || true; exit 1; }

echo "[34/60] GET /metrics with static scrape token also works ..."
SCRAPE_TOKEN="${PCA_OBSERVABILITY_METRICS_TOKEN:-dev-scrape-token-change-me}"
CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $SCRAPE_TOKEN" http://localhost:8080/metrics)
[[ "$CODE" == "200" ]] || { echo "expected 200 with scrape token, got $CODE"; exit 1; }

echo "[35/60] GET /metrics without auth is rejected ..."
CODE=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/metrics)
[[ "$CODE" == "401" ]] || { echo "expected 401 without auth, got $CODE"; exit 1; }

# ---- Slice 11: Vector Memory ----
echo "[36/60] vector search ranks semantically similar memories ..."
VMEM1=$(curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"preference","content":"user loves golang generics"}')
VID1=$(echo "$VMEM1" | jq -r .id)
VMEM2=$(curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"knowledge","content":"deploys via kubernetes helm charts"}')
VID2=$(echo "$VMEM2" | jq -r .id)
[[ -n "$VID1" && "$VID1" != "null" && -n "$VID2" && "$VID2" != "null" ]] \
  || { echo "vector memory create failed"; exit 1; }
VS=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.search","input":{"query":"user loves golang generics","mode":"vector"}}')
TOP_ID=$(echo "$VS" | jq -r '.output.items[0].id')
TOP_SCORE=$(echo "$VS" | jq -r '.output.items[0].score')
[[ "$TOP_ID" == "$VID1" ]] \
  || { echo "vector top-1 mismatch: want $VID1 got $TOP_ID. body: $VS"; exit 1; }
awk -v s="$TOP_SCORE" 'BEGIN{exit !(s>0.9)}' \
  || { echo "expected top score > 0.9, got $TOP_SCORE"; exit 1; }

echo "[37/60] keyword mode falls back to ILIKE ..."
KS=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.search","input":{"query":"kubernetes","mode":"keyword"}}')
echo "$KS" | jq -e --arg id "$VID2" '.output.items[] | select(.id==$id)' >/dev/null \
  || { echo "keyword search missed kubernetes memory: $KS"; exit 1; }

echo "[38/60] Create dedup returns existing id with created=false ..."
DUP=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.save","input":{"type":"preference","content":"user loves golang generics"}}')
DUP_ID=$(echo "$DUP" | jq -r '.output.id')
DUP_CREATED=$(echo "$DUP" | jq -r '.output.created')
[[ "$DUP_ID" == "$VID1" ]] \
  || { echo "dedup id mismatch: want $VID1 got $DUP_ID. body: $DUP"; exit 1; }
[[ "$DUP_CREATED" == "false" ]] \
  || { echo "dedup should set created=false, got $DUP_CREATED"; exit 1; }

echo "[39/60] Distinct content -> new id (no false merge) ..."
NEW=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.save","input":{"type":"knowledge","content":"prefers tabs over spaces in source"}}')
NEW_ID=$(echo "$NEW" | jq -r '.output.id')
NEW_CREATED=$(echo "$NEW" | jq -r '.output.created')
[[ "$NEW_ID" != "$VID1" && "$NEW_ID" != "$VID2" && -n "$NEW_ID" && "$NEW_ID" != "null" ]] \
  || { echo "distinct content should yield new id: $NEW"; exit 1; }
[[ "$NEW_CREATED" == "true" ]] \
  || { echo "distinct content should set created=true, got $NEW_CREATED"; exit 1; }

echo "[40/60] GET /skills lists seeded skills ..."
SKILLS=$(curl -fsS http://localhost:8080/skills -H "Authorization: Bearer $TOK")
SKILL_IDS=$(echo "$SKILLS" | jq -r '.skills[].id' | sort | tr '\n' ',')
echo "  -> ids: $SKILL_IDS"
echo "$SKILL_IDS" | grep -q "e2e-marker," \
  || { echo "expected e2e-marker in skills list, got: $SKILL_IDS"; exit 1; }
echo "$SKILL_IDS" | grep -q "platform-coding-standards," \
  || { echo "expected platform-coding-standards in skills list, got: $SKILL_IDS"; exit 1; }
HAS_BODY=$(echo "$SKILLS" | jq -r '.skills[0] | has("body")')
[[ "$HAS_BODY" == "false" ]] || { echo "List should omit body, got: $SKILLS"; exit 1; }

echo "[41/60] GET /skills/:id?include=body returns body ..."
SK_BODY=$(curl -fsS "http://localhost:8080/skills/e2e-marker?include=body" \
  -H "Authorization: Bearer $TOK" | jq -r '.body')
echo "$SK_BODY" | grep -q "E2E_SKILL_MARKER_V1" \
  || { echo "skill body missing marker token"; exit 1; }

echo "[42/60] agent.run with skill_ids=[e2e-marker] injects marker -> 'skill-marker-ok' ..."
SKRUN=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","skill_ids":["e2e-marker"],"messages":[{"role":"user","content":"hi"}]}')
SK_FINAL=$(echo "$SKRUN" | jq -r '.events[-1].text')
[[ "$SK_FINAL" == "skill-marker-ok" ]] \
  || { echo "expected 'skill-marker-ok', got: $SK_FINAL"; echo "$SKRUN" | head -c 600; exit 1; }

echo "[43/60] sandbox quota exceeded -> 429 quota_exceeded ..."
# Requires PCA_QUOTA_SANDBOX_MAX_ACTIVE=1 in compose (see docker-compose.yml).
QSB1=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
QID1=$(echo "$QSB1" | jq -r .id)
[[ -n "$QID1" && "$QID1" != "null" ]] || { echo "quota test: first sandbox create failed: $QSB1"; exit 1; }
QSB2_CODE=$(curl -sS -o /tmp/qsb2.json -w '%{http_code}' -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
QSB2_ERR=$(jq -r '.error' < /tmp/qsb2.json)
[[ "$QSB2_CODE" == "429" ]] || { echo "quota test: expected 429, got $QSB2_CODE body=$(cat /tmp/qsb2.json)"; exit 1; }
[[ "$QSB2_ERR" == "quota_exceeded" ]] \
  || { echo "quota test: expected error=quota_exceeded, got $QSB2_ERR"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$QID1" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[44/60] POST /auth/logout revokes bearer token (subsequent requests 401) ..."
# Mint a fresh token so we don't invalidate $TOK used by earlier steps.
LTOK=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)
[[ -n "$LTOK" && "$LTOK" != "null" ]] || { echo "logout-test login failed"; exit 1; }
PRELO=$(curl -sS -o /dev/null -w '%{http_code}' http://localhost:8080/sessions \
  -H "Authorization: Bearer $LTOK")
[[ "$PRELO" == "200" ]] || { echo "pre-logout protected GET expected 200, got $PRELO"; exit 1; }
LOUT=$(curl -sS -o /dev/null -w '%{http_code}' -X POST http://localhost:8080/auth/logout \
  -H "Authorization: Bearer $LTOK")
[[ "$LOUT" == "200" ]] || { echo "logout expected 200, got $LOUT"; exit 1; }
POSTLO=$(curl -sS -o /dev/null -w '%{http_code}' http://localhost:8080/sessions \
  -H "Authorization: Bearer $LTOK")
[[ "$POSTLO" == "401" ]] || { echo "post-logout expected 401, got $POSTLO"; exit 1; }

echo "[45/60] POST /sessions auto-binds sandbox; WS list workspace -> fs.list ..."
BIND=$(curl -fsS -X POST http://localhost:8080/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","title":"slice14"}')
BIND_SID=$(echo "$BIND" | jq -r .id)
BIND_SB=$(echo "$BIND" | jq -r .sandbox_id)
[[ -n "$BIND_SID" && "$BIND_SID" != "null" ]] || { echo "bind session create failed: $BIND"; exit 1; }
[[ -n "$BIND_SB" && "$BIND_SB" != "null" ]] || { echo "bind session missing sandbox_id: $BIND"; exit 1; }
GOT=$(curl -fsS "http://localhost:8080/sessions/$BIND_SID" -H "Authorization: Bearer $TOK")
GOT_SB=$(echo "$GOT" | jq -r .sandbox_id)
[[ "$GOT_SB" == "$BIND_SB" ]] || { echo "GET session sandbox_id mismatch: $GOT"; exit 1; }
WS_LIST=$(printf '%s\n' '{"type":"user_message","content":"list files in workspace"}' \
  | docker run --rm -i --network compose_default "$WS_IMG" \
    -H="Authorization: Bearer $TOK" \
    -n1 \
    -t "ws://server:8080/sessions/$BIND_SID/ws" 2>/dev/null \
  | head -n 20 || true)
echo "$WS_LIST" | grep -q '"name":"fs.list"' \
  || { echo "ws missing fs.list tool_call: $WS_LIST"; exit 1; }
sleep 2
BIND_MSGS=$(curl -fsS "http://localhost:8080/sessions/$BIND_SID/messages" -H "Authorization: Bearer $TOK")
echo "$BIND_MSGS" | jq -e '.messages[] | select(.role=="tool")' >/dev/null \
  || { echo "expected tool result after fs.list: $BIND_MSGS"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sessions/$BIND_SID" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[46/60] OIDC authorization code flow -> JWT -> GET /me ..."
OIDC_JAR=$(mktemp 2>/dev/null || echo /tmp/pca-oidc-$$.jar)
OIDC_RESP=$(curl -sS -c "$OIDC_JAR" -b "$OIDC_JAR" -L \
  "http://localhost:8080/auth/oidc/login?tenant=default")
OIDC_TOK=$(echo "$OIDC_RESP" | jq -r .token)
[[ -n "$OIDC_TOK" && "$OIDC_TOK" != "null" ]] || { echo "oidc login failed: $OIDC_RESP"; exit 1; }
ME=$(curl -fsS http://localhost:8080/me -H "Authorization: Bearer $OIDC_TOK")
echo "$ME" | jq -e '.user_id' >/dev/null || { echo "GET /me after oidc failed: $ME"; exit 1; }

echo "[47/60] memory inject on first session message -> audit memory.inject ..."
MEM_INJ=$(curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"knowledge","content":"E2E_SLICE16_INJECT_MARKER prefer golang tabs","tags":["slice16"]}')
MEM_INJ_ID=$(echo "$MEM_INJ" | jq -r .id)
[[ -n "$MEM_INJ_ID" && "$MEM_INJ_ID" != "null" ]] || { echo "slice16 memory create failed: $MEM_INJ"; exit 1; }
INJ_SESS=$(curl -fsS -X POST http://localhost:8080/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","title":"slice16-inject"}')
INJ_SID=$(echo "$INJ_SESS" | jq -r .id)
[[ -n "$INJ_SID" && "$INJ_SID" != "null" ]] || { echo "slice16 session create failed: $INJ_SESS"; exit 1; }
printf '%s\n' '{"type":"user_message","content":"tell me about golang tabs preference"}' \
  | docker run --rm -i --network compose_default "$WS_IMG" \
    -H="Authorization: Bearer $TOK" \
    -n1 \
    -t "ws://server:8080/sessions/$INJ_SID/ws" 2>/dev/null \
  | head -n 15 >/dev/null || true
sleep 2
INJ_AUDIT=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=memory.inject&limit=10")
INJ_HITS=$(echo "$INJ_AUDIT" | jq '.entries | length')
[[ "$INJ_HITS" -ge 1 ]] || { echo "expected >=1 memory.inject audit entry: $INJ_AUDIT"; exit 1; }
INJ_META=$(echo "$INJ_AUDIT" | jq -r '.entries[0].metadata.memory_ids | length')
[[ "$INJ_META" -ge 1 ]] || { echo "memory.inject metadata missing memory_ids: $INJ_AUDIT"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sessions/$INJ_SID" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[48/60] sandbox files list API returns workspace entries ..."
FILES_SESS=$(curl -fsS -X POST http://localhost:8080/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","title":"slice16-files"}')
FILES_SB=$(echo "$FILES_SESS" | jq -r .sandbox_id)
[[ -n "$FILES_SB" && "$FILES_SB" != "null" ]] || { echo "slice16 files session missing sandbox_id: $FILES_SESS"; exit 1; }
HELLO_B64=$(printf 'hello slice16' | base64 | tr -d '\n')
curl -fsS -X PUT "http://localhost:8080/sandbox/sessions/$FILES_SB/files?path=hello.txt" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"content_base64\":\"$HELLO_B64\"}" >/dev/null
FLIST=$(curl -fsS "http://localhost:8080/sandbox/sessions/$FILES_SB/files?path=.&list=1" \
  -H "Authorization: Bearer $TOK")
echo "$FLIST" | jq -e '.entries[] | select(.name=="hello.txt")' >/dev/null \
  || { echo "files list missing hello.txt: $FLIST"; exit 1; }
FILES_SID=$(echo "$FILES_SESS" | jq -r .id)
curl -fsS -X DELETE "http://localhost:8080/sessions/$FILES_SID" \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[49/60] tenant DB skill (admin CRUD) flows through resolver -> 'tenant-skill-marker-ok' ..."
TS_BODY='E2E_TENANT_SKILL_V1\nThis skill is created via the admin API; the resolver should pick it up alongside FS skills.'
TS_PAYLOAD=$(printf '{"skill_key":"e2e-tenant-marker","description":"tenant marker","body":"%s"}' "$TS_BODY")
TS_CREATE=$(curl -sS -o /tmp/ts_create.json -w '%{http_code}' -X POST http://localhost:8080/admin/skills \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d "$TS_PAYLOAD")
[[ "$TS_CREATE" == "201" ]] || { echo "expected 201 from admin skill create, got $TS_CREATE body=$(cat /tmp/ts_create.json)"; exit 1; }
TS_RUN=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","skill_ids":["e2e-tenant-marker"],"messages":[{"role":"user","content":"hi"}]}')
TS_FINAL=$(echo "$TS_RUN" | jq -r '.events[-1].text')
[[ "$TS_FINAL" == "tenant-skill-marker-ok" ]] \
  || { echo "expected 'tenant-skill-marker-ok', got: $TS_FINAL"; echo "$TS_RUN" | head -c 600; exit 1; }
curl -fsS -X PUT http://localhost:8080/admin/profiles/coding/skills \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"skill_keys":["e2e-tenant-marker"]}' >/dev/null
TS_BIND=$(curl -fsS http://localhost:8080/admin/profiles/coding/skills \
  -H "Authorization: Bearer $TOK" | jq -r '.skill_keys | join(",")')
[[ "$TS_BIND" == "e2e-tenant-marker" ]] \
  || { echo "expected binding [e2e-tenant-marker], got: $TS_BIND"; exit 1; }
curl -fsS -X PUT http://localhost:8080/admin/profiles/coding/skills \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"skill_keys":[]}' >/dev/null
curl -fsS -X DELETE http://localhost:8080/admin/skills/e2e-tenant-marker \
  -H "Authorization: Bearer $TOK" >/dev/null

echo "[50/60] sub-agent delegate round-trip + GET /agent/profiles ..."
# 50a: registry lists all 4 profiles, each with a description
PROFS=$(curl -fsS -H "Authorization: Bearer $TOK" http://localhost:8080/agent/profiles)
PROF_NAMES=$(echo "$PROFS" | jq -r '.profiles[].name' | sort | tr '\n' ',')
[[ "$PROF_NAMES" == "coding,research,review,workflow-authoring," ]] \
  || { echo "profiles list mismatch: $PROF_NAMES"; exit 1; }
EMPTY_DESC=$(echo "$PROFS" | jq -r '[.profiles[] | select(.description == "" or .description == null)] | length')
[[ "$EMPTY_DESC" == "0" ]] || { echo "profiles missing description: $PROFS"; exit 1; }

# 50b: drive a delegate chain via /agent/run with the parent marker.
# Mock provider first turn -> tool_call agent.delegate{profile:"review",...};
# child review run emits "delegate-sub-marker-ok"; parent finalises with the
# sub result embedded in the text.
DRUN=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","messages":[{"role":"user","content":"E2E_DELEGATE_PARENT_V1 please delegate to review profile"}]}')
DKINDS=$(echo "$DRUN" | jq -r '.events[].kind' | tr '\n' ',')
echo "  -> events: $DKINDS"
echo "$DKINDS" | grep -q "tool_call," \
  || { echo "delegate: no tool_call in events: $DRUN"; exit 1; }
echo "$DKINDS" | grep -q "tool_result," \
  || { echo "delegate: no tool_result in events: $DRUN"; exit 1; }
DTOOL=$(echo "$DRUN" | jq -r '[.events[] | select(.kind=="tool_call")][0].tool_name')
[[ "$DTOOL" == "agent.delegate" ]] \
  || { echo "delegate: tool_call tool_name mismatch: $DTOOL"; exit 1; }
DLAST=$(echo "$DRUN" | jq -r '.events[-1].kind')
[[ "$DLAST" == "final" ]] || { echo "delegate: expected final, got $DLAST"; exit 1; }
DFINAL=$(echo "$DRUN" | jq -r '.events[-1].text')
echo "$DFINAL" | grep -q "delegate-parent-final" \
  || { echo "delegate: final missing parent marker: $DFINAL"; exit 1; }
echo "$DFINAL" | grep -q "delegate-sub-marker-ok" \
  || { echo "delegate: final missing sub marker: $DFINAL"; exit 1; }

# 50c: tool_result content parses to {result, status:"ok", sub_steps>=1}
DRESULT=$(echo "$DRUN" | jq -c '[.events[] | select(.kind=="tool_result")][0].tool_output')
DRES_STATUS=$(echo "$DRESULT" | jq -r '.status')
DRES_SUB_STEPS=$(echo "$DRESULT" | jq -r '.sub_steps')
DRES_TEXT=$(echo "$DRESULT" | jq -r '.result')
[[ "$DRES_STATUS" == "ok" ]] \
  || { echo "delegate: status not ok: $DRESULT"; exit 1; }
[[ "$DRES_SUB_STEPS" =~ ^[1-9][0-9]*$ ]] \
  || { echo "delegate: sub_steps not positive int: $DRES_SUB_STEPS"; exit 1; }
echo "$DRES_TEXT" | grep -q "delegate-sub-marker-ok" \
  || { echo "delegate: tool_result.result missing sub marker: $DRES_TEXT"; exit 1; }

# 50d: audit has both delegate.start and delegate.complete with sub_profile=review
sleep 1
DSTART=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=agent.delegate.start&limit=10" \
  | jq '[.entries[] | select(.metadata.sub_profile=="review")] | length')
DCOMP=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=agent.delegate.complete&limit=10" \
  | jq '[.entries[] | select(.metadata.sub_profile=="review")] | length')
[[ "$DSTART" -ge 1 ]] \
  || { echo "delegate: expected >=1 agent.delegate.start entry with sub_profile=review"; exit 1; }
[[ "$DCOMP" -ge 1 ]] \
  || { echo "delegate: expected >=1 agent.delegate.complete entry with sub_profile=review"; exit 1; }

echo "[57/60] workflow CRUD + publish registers workflow.e2e-demo into ToolBus ..."
WF_DSL=$(cat <<'YAML'
id: e2e-demo
name: E2E Workflow Demo
description: Slice 19 smoke
inputs:
  name: { type: string, default: "world" }
steps:
  - id: build_msg
    assign:
      msg: "hello ${inputs.name}"
  - id: shell_step
    use: shell.exec
    args:
      sandbox_id: "00000000-0000-0000-0000-000000000000"
      cmd: "echo workflowmock"
    on_error: continue
outputs:
  said: "${vars.msg}"
  shell_out: "${steps.shell_step.output}"
YAML
)
WF_PAYLOAD=$(jq -n --arg dsl "$WF_DSL" \
  '{slug:"e2e-demo", name:"E2E Workflow Demo", description:"Slice 19 smoke", dsl_yaml:$dsl}')
WF_CREATE=$(curl -sS -o /tmp/wf_create.json -w '%{http_code}' \
  -X POST http://localhost:8080/admin/workflows \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d "$WF_PAYLOAD")
[[ "$WF_CREATE" == "201" ]] \
  || { echo "expected 201 from workflow create, got $WF_CREATE body=$(cat /tmp/wf_create.json)"; exit 1; }
WF_GET=$(curl -fsS http://localhost:8080/admin/workflows/e2e-demo \
  -H "Authorization: Bearer $TOK")
WF_PUB=$(echo "$WF_GET" | jq -r '.published')
[[ "$WF_PUB" == "false" ]] || { echo "freshly created workflow should be unpublished, got: $WF_PUB"; exit 1; }
WF_PUBLISH=$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST http://localhost:8080/admin/workflows/e2e-demo/publish \
  -H "Authorization: Bearer $TOK")
[[ "$WF_PUBLISH" == "204" ]] || { echo "expected 204 from publish, got $WF_PUBLISH"; exit 1; }
WF_IN_TOOLS=$(curl -fsS -H "Authorization: Bearer $TOK" http://localhost:8080/tools \
  | jq -r '[.tools[].name] | index("workflow.e2e-demo")')
[[ "$WF_IN_TOOLS" != "null" ]] || { echo "workflow.e2e-demo missing from /tools after publish"; exit 1; }

echo "[58/60] /tools/invoke workflow.e2e-demo records a workflow_runs row ..."
WF_INVOKE=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"workflow.e2e-demo","input":{"name":"E2E"}}')
WF_SAID=$(echo "$WF_INVOKE" | jq -r '.output.said')
[[ "$WF_SAID" == "hello E2E" ]] \
  || { echo "expected outputs.said='hello E2E', got: $WF_INVOKE"; exit 1; }
sleep 1
WF_REAL_RUNS=$(docker compose exec -T postgres psql -U app -d app -tA -c \
  "SELECT count(*) FROM workflow_runs r JOIN workflows w ON r.workflow_id=w.id \
   WHERE w.slug='e2e-demo' AND r.status='ok' AND r.dry_run=false;")
[[ "$WF_REAL_RUNS" -ge 1 ]] \
  || { echo "expected >=1 workflow_runs row for e2e-demo (status=ok, dry_run=false), got: $WF_REAL_RUNS"; exit 1; }

echo "[59/60] agent.run with workflow marker fires workflow.e2e-demo tool_call ..."
AWRUN=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","messages":[{"role":"user","content":"E2E_WORKFLOW_V1 please invoke the demo workflow"}]}')
AWKINDS=$(echo "$AWRUN" | jq -r '.events[].kind' | tr '\n' ',')
echo "  -> events: $AWKINDS"
echo "$AWKINDS" | grep -q "tool_call," \
  || { echo "workflow agent: no tool_call in events: $AWRUN"; exit 1; }
echo "$AWKINDS" | grep -q "tool_result," \
  || { echo "workflow agent: no tool_result in events: $AWRUN"; exit 1; }
AW_TOOL=$(echo "$AWRUN" | jq -r '[.events[] | select(.kind=="tool_call")][0].tool_name')
[[ "$AW_TOOL" == "workflow.e2e-demo" ]] \
  || { echo "workflow agent: expected tool_call workflow.e2e-demo, got: $AW_TOOL"; exit 1; }
AW_FINAL=$(echo "$AWRUN" | jq -r '.events[-1].text')
echo "$AW_FINAL" | grep -q "workflow-tool-final" \
  || { echo "workflow agent: final missing marker: $AW_FINAL"; exit 1; }
sleep 1
WF_AUD_START=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=workflow.invoke.start&limit=10" \
  | jq '[.entries[] | select(.target=="e2e-demo")] | length')
WF_AUD_DONE=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=workflow.invoke.complete&limit=10" \
  | jq '[.entries[] | select(.target=="e2e-demo")] | length')
[[ "$WF_AUD_START" -ge 1 ]] \
  || { echo "workflow: expected >=1 workflow.invoke.start audit entry for e2e-demo"; exit 1; }
[[ "$WF_AUD_DONE" -ge 1 ]] \
  || { echo "workflow: expected >=1 workflow.invoke.complete audit entry for e2e-demo"; exit 1; }

echo "[60/60] dry-run mocks mutating tool nodes ..."
WF_DRY=$(curl -fsS -X POST 'http://localhost:8080/admin/workflows/e2e-demo/invoke' \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"inputs":{"name":"DryRun"}, "dry_run": true}')
WF_DRY_STATUS=$(echo "$WF_DRY" | jq -r '.status')
[[ "$WF_DRY_STATUS" == "ok" ]] || { echo "dry-run status not ok: $WF_DRY"; exit 1; }
WF_DRY_FLAG=$(echo "$WF_DRY" | jq -r '.dry_run')
[[ "$WF_DRY_FLAG" == "true" ]] || { echo "dry-run flag not true: $WF_DRY"; exit 1; }
WF_DRY_SHELL=$(echo "$WF_DRY" | jq -c '.outputs.shell_out')
echo "$WF_DRY_SHELL" | grep -q '"dry_run":true' \
  || { echo "dry-run shell.exec output missing dry_run marker: $WF_DRY_SHELL"; exit 1; }
echo "$WF_DRY_SHELL" | grep -q '"tool":"shell.exec"' \
  || { echo "dry-run shell.exec output missing tool name: $WF_DRY_SHELL"; exit 1; }
sleep 1
WF_DRY_RUNS=$(docker compose exec -T postgres psql -U app -d app -tA -c \
  "SELECT count(*) FROM workflow_runs r JOIN workflows w ON r.workflow_id=w.id \
   WHERE w.slug='e2e-demo' AND r.dry_run=true;")
[[ "$WF_DRY_RUNS" -ge 1 ]] \
  || { echo "expected >=1 dry_run=true workflow_runs row, got: $WF_DRY_RUNS"; exit 1; }

# cleanup so reruns of test-e2e.sh stay idempotent
curl -fsS -X POST http://localhost:8080/admin/workflows/e2e-demo/unpublish \
  -H "Authorization: Bearer $TOK" >/dev/null
curl -fsS -X DELETE http://localhost:8080/admin/workflows/e2e-demo \
  -H "Authorization: Bearer $TOK" >/dev/null

echo
echo "E2E PASS"

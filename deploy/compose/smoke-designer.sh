#!/usr/bin/env bash
# A4: post-deploy designer smoke — rebuild server (embeds WebUI) and run Playwright.
set -euo pipefail
cd "$(dirname "$0")"

echo "==> docker compose build server"
docker compose build server

echo "==> recreate server container"
docker compose up -d --force-recreate server

echo "==> wait for healthz"
for i in $(seq 1 60); do
  if curl -fsS http://localhost:8080/healthz >/dev/null 2>&1; then
    echo "  server healthy"
    break
  fi
  if [[ "$i" -eq 60 ]]; then
    echo "healthz timeout" >&2
    exit 1
  fi
  sleep 2
done

echo "==> Playwright designer smoke (internal/webui)"
ROOT="$(cd ../.. && pwd)"
cd "$ROOT/internal/webui"
if [[ ! -d node_modules/@playwright/test ]]; then
  npm ci
  npx playwright install chromium
fi
npm run test:e2e:designer

echo "==> A4 OK — hard-refresh browser (Ctrl+Shift+R) when testing UI manually"

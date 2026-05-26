# WebUI Playwright smoke (Slice 21e A3)

Requires a running PCA server (compose on `localhost:8080`) and demo user (`demo@example.com` / `demo123`).

```bash
# Terminal 1
cd deploy/compose && docker compose up -d

# Terminal 2
cd internal/webui
npm install
npx playwright install chromium
npm run test:e2e:designer
```

Environment:

| Variable | Default |
|----------|---------|
| `PCA_E2E_BASE_URL` | `http://localhost:8080` |
| `PCA_E2E_WORKFLOW_SLUG` | `e2e-mock-chain` |
| `PCA_E2E_EMAIL` | `demo@example.com` |
| `PCA_E2E_PASSWORD` | `demo123` |

Quick screenshot script (no test runner): `python scripts/debug-workflow-designer.py`

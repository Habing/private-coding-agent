# pca Helm chart

Private Coding Agent server + K8sDriver sandbox model.

## Quick install

```bash
kubectl create ns pca-system
kubectl create ns pca-sandboxes        # only if rbac.createSandboxNamespace=false
helm install pca ./deploy/helm/pca \
  -n pca-system \
  --set secrets.jwtSecret="$(openssl rand -hex 32)"
kubectl -n pca-system wait --for=condition=available --timeout=180s deploy/pca-server
kubectl -n pca-system port-forward svc/pca-server 8080:8080
curl http://localhost:8080/healthz
```

## Layout

- `Chart.yaml`, `values.yaml` — defaults safe for staging
- `values-kind.yaml` — overrides used by `.github/workflows/kind-nightly.yml`
- `templates/` — Deployment + Service + ConfigMap + Secret + RBAC + Postgres + Redis + 3 NetworkPolicies
- `test/` — `kind-config.yaml` + `kind-e2e.sh` invoked by the nightly workflow

## Two namespaces

The release namespace holds the server and (optionally) chart-managed PG/Redis.
`rbac.sandboxNamespace` (default `pca-sandboxes`) is where K8sDriver spawns
sandbox Pods. Server RBAC is scoped to that namespace only — if the server
process is compromised, the SA token only grants `pods` and `pods/exec`
within `pca-sandboxes`.

## Key values

| Key | Default | Notes |
|-----|---------|-------|
| `secrets.jwtSecret` | "" (required) | >=32 chars; chart fails to render when blank |
| `secrets.existing` | "" | Name of an existing Secret to reference instead of chart-managed one |
| `postgres.enabled` | true | false → set `postgres.externalDsn` |
| `redis.enabled` | true | false → set `redis.externalAddr` |
| `sandbox.network` | internal | `internal` \| `bridge` \| `none` — affects which NP applies |
| `networkPolicy.enabled` | true | false → skip every NP (dev only) |
| `config.sandbox.k8s.namespace` | pca-sandboxes | Must equal `rbac.sandboxNamespace` |
| `config.workflow.*` | see values.yaml | Slice 24 trigger scheduler + runs retention (mirrors compose) |
| `config.orchestrator.rules` | nl-workflow-author | Pre-Run routing hints; extend in values override |

See `docs/DEPLOY-K8S.md` for the full production checklist and troubleshooting.

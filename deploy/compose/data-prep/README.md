# data-prep volumes (compose)

`mcp-data-prep` uses **Docker named volumes** (`data_prep_inbox`, `data_prep_outbox`), not host bind mounts.

This avoids Docker Desktop on Windows failing on `F:\` paths:

`mkdir /run/desktop/mnt/host/f: file exists`

## Seed data

On first start, the image copies `batch.sample.json` into the inbox volume if empty.

## Add your own files

```bash
cd deploy/compose
CID=$(docker compose ps -q mcp-data-prep)
docker cp ./my-batch.json "$CID:/data/inbox/"
docker compose exec mcp-data-prep ls -la /data/inbox
```

## Reset volumes

```bash
docker compose down -v   # drops data_prep_* volumes
docker compose up -d --build mcp-data-prep
```

#!/bin/sh
set -e
mkdir -p /data/inbox /data/outbox
chown -R nonroot:nonroot /data/inbox /data/outbox
if [ ! -f /data/inbox/batch.sample.json ] && [ -f /opt/seed/batch.sample.json ]; then
  cp /opt/seed/batch.sample.json /data/inbox/
  chown nonroot:nonroot /data/inbox/batch.sample.json
fi
exec su-exec nonroot /app/mcp-data-prep

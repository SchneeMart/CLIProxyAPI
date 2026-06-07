#!/bin/bash
# Restart-Skript nach CLIProxy v6 → v7 Update
# Führe das aus, NACHDEM du die Session, die mich ausführt, beendet hast
# (sonst killst du deine eigene Connection)

set -e
cd /mnt/l/DockerContainer/CLIProxy

echo "=== 1. Working Tree Status ==="
git status --short | head -5

echo ""
echo "=== 2. Aktiver Container (sollte NICHTS anzeigen, wenn er läuft) ==="
docker ps --filter "name=cli-proxy-api" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

echo ""
echo "=== 3. Binary-Check ==="
ls -la ./bin/CLIProxyAPI
file ./bin/CLIProxyAPI

echo ""
echo "=== 4. Backup des alten Containers (Image) ==="
docker commit cli-proxy-api cli-proxy-api:pre-v7-backup 2>/dev/null || echo "Container läuft nicht, kein Backup nötig"

echo ""
echo "=== 5. Restart ==="
docker compose down
docker compose up -d

echo ""
echo "=== 6. Warte 5s und check Logs ==="
sleep 5
docker logs --tail 30 cli-proxy-api 2>&1

echo ""
echo "=== 7. Smoke-Test ==="
curl -sS http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer eb8de86c2d0da5f47ef7d417db7cc4cd" \
  -H "Content-Type: application/json" \
  -d '{"model":"default","messages":[{"role":"user","content":"Sag Hallo in einem Satz."}],"max_tokens":50}' | head -30

echo ""
echo "=== Fertig. Falls Smoke-Test oben JSON liefert, läuft alles. ==="

#!/bin/bash
# Schnelles lokales Build mit Go
set -e

export PATH=$PATH:/usr/local/go/bin

echo "Building CLI-Proxy API..."
mkdir -p bin

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o ./bin/CLIProxyAPI \
    ./cmd/server/

echo "Build complete: ./bin/CLIProxyAPI"
echo "Starte mit: docker compose restart cli-proxy-api"

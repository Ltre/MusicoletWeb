#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT"
mkdir -p bin
echo "[MusicoletWeb] Building Termux/Android arm64 read-only agent..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/musicolet-agent-arm64 ./cmd/agent
echo "[MusicoletWeb] bin/musicolet-agent-arm64"

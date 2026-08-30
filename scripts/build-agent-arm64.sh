#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT"
mkdir -p bin
VERSION="${MUSICOLET_BUILD_VERSION:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
echo "[MusicoletWeb] Building Termux/Android arm64 read-only agent ($VERSION)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o bin/musicolet-agent-arm64 ./cmd/agent
echo "[MusicoletWeb] bin/musicolet-agent-arm64"

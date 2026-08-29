#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./...
go test -tags=integration ./internal/db
node --check web/app.js
node --check web/now-playing.js
bash -n scripts/linux-alyhk.start.sh
bash -n scripts/build-agent-arm64.sh

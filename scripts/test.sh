#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./...
go vet ./...
go test -tags=integration ./internal/db ./internal/musicolet
for f in web/*.js; do node --check "$f"; done
bash -n scripts/linux-alyhk.start.sh
bash -n scripts/build-agent-arm64.sh
bash -n scripts/test.sh

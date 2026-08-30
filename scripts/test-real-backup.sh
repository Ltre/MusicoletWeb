#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "Usage: $0 <2026-08-30-backup.zip> [base-backup.zip]" >&2
  exit 2
fi
export MUSICOLET_REAL_BACKUP="$1"
if [[ $# -eq 2 ]]; then
  export MUSICOLET_REAL_BASE_BACKUP="$2"
fi

go test -tags=integration ./internal/musicolet -run '^TestRealMusicoletBackup' -v
if [[ $# -eq 2 ]]; then
  go test -tags=integration ./internal/app -run '^TestRealImportProcedureV1ServerMV2$' -v
fi

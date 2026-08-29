#!/usr/bin/env bash
set -u

EXIT_CODE=1
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
fail(){ echo >&2; echo "[MusicoletWeb] $1" >&2; exit "${2:-1}"; }
[ -f "$REPO_ROOT/go.mod" ] || fail "Repository root could not be resolved from: $SCRIPT_DIR"
cd "$REPO_ROOT" || fail "Failed to enter repository root"
export MUSICOLET_BIND_HOST="${MUSICOLET_BIND_HOST:-0.0.0.0}"
export MUSICOLET_PORT="${MUSICOLET_PORT:-4001}"
export MUSICOLET_DATA_DIR="${MUSICOLET_DATA_DIR:-$REPO_ROOT/data}"
CONFIG_FILE="$MUSICOLET_DATA_DIR/config.env"
mkdir -p "$MUSICOLET_DATA_DIR" "$REPO_ROOT/bin" || fail "Failed to create data/bin directories"
if [ ! -f "$CONFIG_FILE" ]; then
cat >"$CONFIG_FILE" <<'CONFIGEOF'
# MusicoletWeb private runtime configuration. data/ is ignored by Git.
MUSICOLET_ADMIN_USERNAME=admin
MUSICOLET_ADMIN_PASSWORD=
# Base32 secret used by Google Authenticator/TOTP.
MUSICOLET_ADMIN_TOTP_SECRET=
# Use long random values for both keys below.
MUSICOLET_SESSION_KEY=
MUSICOLET_AGENT_TOKEN=
MUSICOLET_PUBLIC_BASE_URL=
CONFIGEOF
chmod 600 "$CONFIG_FILE" 2>/dev/null || true
echo "[MusicoletWeb] Created config template: $CONFIG_FILE"
fi
set -a
# shellcheck disable=SC1090
. "$CONFIG_FILE"
set +a
command -v go >/dev/null 2>&1 || fail "Go was not found in PATH"
command -v git >/dev/null 2>&1 || fail "Git was not found in PATH"
echo "[MusicoletWeb] Repository: $REPO_ROOT"
echo "[MusicoletWeb] Listening: http://$MUSICOLET_BIND_HOST:$MUSICOLET_PORT/"
echo "[MusicoletWeb] Data: $MUSICOLET_DATA_DIR"
echo "[MusicoletWeb] Tidying Go modules..."
go mod tidy || fail "go mod tidy failed; check DNS/network/proxy"
echo "[MusicoletWeb] Downloading dependencies..."
go mod download all || fail "go mod download failed"
go mod verify || fail "go mod verify failed"
echo "[MusicoletWeb] Rebuilding server..."
go build -trimpath -o "$REPO_ROOT/bin/musicoletweb" ./cmd/server || fail "server build failed"
echo "[MusicoletWeb] Starting..."
"$REPO_ROOT/bin/musicoletweb"
EXIT_CODE=$?
[ "$EXIT_CODE" -eq 0 ] || fail "server exited with code $EXIT_CODE" "$EXIT_CODE"

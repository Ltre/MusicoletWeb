#!/usr/bin/env bash
set -u

# MusicoletWeb launcher for the Alibaba Cloud Hong Kong deployment.
# It rebuilds both binaries before every start. Secrets live in data/config.env.

EXIT_CODE=1
export MUSICOLET_BIND_HOST="0.0.0.0"
export MUSICOLET_PORT="4001"
export MUSICOLET_DEV_AUTH_ENABLED="0"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"

fail() {
    local message="$1"
    local code="${2:-1}"
    echo >&2
    echo "[MusicoletWeb] $message" >&2
    exit "$code"
}

[ -f "$REPO_ROOT/go.mod" ] || fail "Repository root could not be resolved from: $SCRIPT_DIR"
cd "$REPO_ROOT" || fail "Failed to enter repository root: $REPO_ROOT"

export MUSICOLET_DATA_DIR="$REPO_ROOT/data"
MUSICOLET_CONFIG_FILE="$MUSICOLET_DATA_DIR/config.env"
MUSICOLET_BIN_DIR="$MUSICOLET_DATA_DIR/bin"
mkdir -p "$MUSICOLET_DATA_DIR" "$MUSICOLET_BIN_DIR" || fail "Failed to create runtime directories."

if [ ! -f "$MUSICOLET_CONFIG_FILE" ]; then
    umask 077
    cat > "$MUSICOLET_CONFIG_FILE" <<'CONFIGEOF'
# MusicoletWeb private runtime configuration. data/ is ignored by Git.
MUSICOLET_ADMIN_USERNAME=admin
MUSICOLET_ADMIN_PASSWORD=
MUSICOLET_ADMIN_TOTP_SECRET=
MUSICOLET_SESSION_KEY=
MUSICOLET_AGENT_TOKEN=
MUSICOLET_PUBLIC_BASE_URL=https://musicolet.miku.us
CONFIGEOF
    chmod 600 "$MUSICOLET_CONFIG_FILE" 2>/dev/null || true
    echo "[MusicoletWeb] Created configuration template: $MUSICOLET_CONFIG_FILE"
    fail "Startup paused. Fill the password, Base32 TOTP secret, session key, and agent token, then run this script again." 2
fi

set -a
# shellcheck disable=SC1090
. "$MUSICOLET_CONFIG_FILE"
set +a
# This launcher is production-only even if config.env contains a stale development value.
export MUSICOLET_DEV_AUTH_ENABLED="0"

[ -n "${MUSICOLET_ADMIN_USERNAME:-}" ] || fail "MUSICOLET_ADMIN_USERNAME is empty in: $MUSICOLET_CONFIG_FILE" 2
[ -n "${MUSICOLET_ADMIN_PASSWORD:-}" ] || fail "MUSICOLET_ADMIN_PASSWORD is empty in: $MUSICOLET_CONFIG_FILE" 2
[ "${#MUSICOLET_ADMIN_PASSWORD}" -ge 12 ] || fail "MUSICOLET_ADMIN_PASSWORD must contain at least 12 characters." 2
[ -n "${MUSICOLET_ADMIN_TOTP_SECRET:-}" ] || fail "MUSICOLET_ADMIN_TOTP_SECRET is empty in: $MUSICOLET_CONFIG_FILE" 2
[ -n "${MUSICOLET_SESSION_KEY:-}" ] || fail "MUSICOLET_SESSION_KEY is empty in: $MUSICOLET_CONFIG_FILE" 2
[ "${#MUSICOLET_SESSION_KEY}" -ge 32 ] || fail "MUSICOLET_SESSION_KEY must contain at least 32 characters." 2
[ -n "${MUSICOLET_AGENT_TOKEN:-}" ] || fail "MUSICOLET_AGENT_TOKEN is empty in: $MUSICOLET_CONFIG_FILE" 2
[ "${#MUSICOLET_AGENT_TOKEN}" -ge 24 ] || fail "MUSICOLET_AGENT_TOKEN must contain at least 24 characters." 2

command -v go >/dev/null 2>&1 || fail "Go was not found in PATH."
command -v git >/dev/null 2>&1 || fail "Git was not found in PATH."

echo "[MusicoletWeb] Repository root: $REPO_ROOT"
echo "[MusicoletWeb] Listening on http://$MUSICOLET_BIND_HOST:$MUSICOLET_PORT/"
echo "[MusicoletWeb] Data directory: $MUSICOLET_DATA_DIR"
echo "[MusicoletWeb] Configuration: $MUSICOLET_CONFIG_FILE"
echo

echo "[MusicoletWeb] Tidying Go module metadata..."
go mod tidy || fail "go mod tidy failed. Check outbound network/DNS/proxy settings."
echo "[MusicoletWeb] Downloading Go module dependencies..."
go mod download all || fail "Dependency download failed."
echo "[MusicoletWeb] Verifying Go module dependencies..."
go mod verify || fail "Go module verification failed."

echo "[MusicoletWeb] Rebuilding server..."
CGO_ENABLED=0 go build -trimpath -o "$MUSICOLET_BIN_DIR/musicoletweb" ./cmd/musicoletweb || fail "Server build failed."
echo "[MusicoletWeb] Rebuilding Termux arm64 agent..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o "$MUSICOLET_BIN_DIR/musicolet-agent-linux-arm64" ./cmd/musicolet-agent || fail "Agent cross-build failed."

echo "[MusicoletWeb] Starting server..."
"$MUSICOLET_BIN_DIR/musicoletweb"
EXIT_CODE=$?
if [ "$EXIT_CODE" -ne 0 ]; then
    fail "MusicoletWeb exited with code $EXIT_CODE." "$EXIT_CODE"
fi
exit 0

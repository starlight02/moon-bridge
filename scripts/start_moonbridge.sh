#!/usr/bin/env bash
# Build and start the Moon Bridge server in background.
# Saves connection info to .moonbridge.env for client scripts to consume.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_FILE="${MOONBRIDGE_CONFIG:-"${ROOT_DIR}/config.yml"}"
SERVER_BIN="${ROOT_DIR}/.cache/moonbridge/moonbridge"
CACHE_DIR="${ROOT_DIR}/.cache/moonbridge"
LOG_FILE="${ROOT_DIR}/logs/moonbridge.log"
ENV_FILE="${ROOT_DIR}/.moonbridge.env"

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  echo "Do not source this script; run it directly." >&2
  return 1
fi

mkdir -p "$(dirname "$LOG_FILE")" "$CACHE_DIR"

log() { printf '%s\n' "$*"; }
log_error() { printf '%s\n' "$*" >&2; }

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log_error "missing required command: $1"
    exit 1
  fi
}

require_command go

if [[ ! -f "$CONFIG_FILE" ]]; then
  log_error "missing config file: ${CONFIG_FILE}"
  log_error "copy config.example.yml to config.yml and fill provider settings"
  exit 1
fi

# Build.
log "Building Moon Bridge"
(
  cd "$ROOT_DIR"
  go build -o "$SERVER_BIN" ./cmd/moonbridge
)

# Validate mode and extract metadata.
MODE="$("$SERVER_BIN" --config "$CONFIG_FILE" --print-mode 2>/dev/null)"
ADDR="$("$SERVER_BIN" --config "$CONFIG_FILE" --print-addr 2>/dev/null)"
DEFAULT_MODEL="$("$SERVER_BIN" --config "$CONFIG_FILE" --print-default-model 2>/dev/null || true)"
CODEX_MODEL="$("$SERVER_BIN" --config "$CONFIG_FILE" --print-codex-model 2>/dev/null || true)"
CLAUDE_MODEL="$("$SERVER_BIN" --config "$CONFIG_FILE" --print-claude-model 2>/dev/null || true)"

# Parse addr.
if [[ "$ADDR" == :* ]]; then
  HOST="127.0.0.1"
  PORT="${ADDR#:}"
else
  HOST="${ADDR%:*}"
  PORT="${ADDR##*:}"
fi

# Check port is free.
if (echo > "/dev/tcp/${HOST}/${PORT}") >/dev/null 2>&1; then
  log_error "port already in use: ${ADDR}"
  log_error "stop the existing process or change server.addr in config.yml"
  exit 1
fi

# Write env file for client scripts.
cat > "$ENV_FILE" <<EOF
MOONBRIDGE_ADDR="${ADDR}"
MOONBRIDGE_MODE="${MODE}"
MOONBRIDGE_DEFAULT_MODEL="${DEFAULT_MODEL}"
MOONBRIDGE_CODEX_MODEL="${CODEX_MODEL}"
MOONBRIDGE_CLAUDE_MODEL="${CLAUDE_MODEL}"
MOONBRIDGE_CONFIG_FILE="${CONFIG_FILE}"
MOONBRIDGE_SERVER_BIN="${SERVER_BIN}"
MOONBRIDGE_LOG_FILE="${LOG_FILE}"
EOF

# Register cleanup.
cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    log "Stopping Moon Bridge"
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
    log "Moon Bridge stopped"
    rm -f "$ENV_FILE"
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

# Start server.
log "Starting Moon Bridge on ${ADDR}"
log "Moon Bridge log: ${LOG_FILE}"
(
  cd "$ROOT_DIR"
  "$SERVER_BIN" --config "$CONFIG_FILE"
) >> "$LOG_FILE" 2>&1 &
SERVER_PID="$!"

# Wait for server.
deadline=$((SECONDS + 30))
while (( SECONDS < deadline )); do
  if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    log_error "Moon Bridge exited before it became ready"
    exit 1
  fi
  if (echo > "/dev/tcp/${HOST}/${PORT}") >/dev/null 2>&1; then
    log "Moon Bridge is ready on ${ADDR}"
    break
  fi
  sleep 0.2
done

if (( SECONDS >= deadline )); then
  log_error "Moon Bridge did not start on ${ADDR}"
  exit 1
fi

# Wait for the server process to finish (blocking).
log "Server running in background (PID ${SERVER_PID}). Press Ctrl+C to stop."
wait "$SERVER_PID"

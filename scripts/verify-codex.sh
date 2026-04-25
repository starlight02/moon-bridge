#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_FILE="${MOONBRIDGE_CONFIG:-"${ROOT_DIR}/config.yml"}"
CODEX_HOME_DIR="${CODEX_HOME:-"${ROOT_DIR}/verify-codex-home"}"
HOST="${MOONBRIDGE_VERIFY_HOST:-127.0.0.1}"
PORT="${MOONBRIDGE_VERIFY_PORT:-18080}"
ADDR="${HOST}:${PORT}"
MODEL_ALIAS="${MOONBRIDGE_VERIFY_MODEL_ALIAS:-moonbridge}"
PROMPT="${1:-Reply exactly: moonbridge codex ok}"

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "missing required command: ${command_name}" >&2
    exit 1
  fi
}

wait_for_server() {
  local deadline=$((SECONDS + 30))
  while (( SECONDS < deadline )); do
    if (echo > "/dev/tcp/${HOST}/${PORT}") >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "Moon Bridge did not start on ${ADDR}" >&2
  return 1
}

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_command go
require_command codex

if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "missing config file: ${CONFIG_FILE}" >&2
  echo "copy config.example.yml to config.yml and fill provider settings" >&2
  exit 1
fi

mkdir -p "$CODEX_HOME_DIR" "${ROOT_DIR}/.cache/go-build"

export MOONBRIDGE_CONFIG="$CONFIG_FILE"
export CGO_ENABLED="${CGO_ENABLED:-0}"
export GOCACHE="${GOCACHE:-"${ROOT_DIR}/.cache/go-build"}"

echo "Starting Moon Bridge on ${ADDR}"
(
  cd "$ROOT_DIR"
  go run ./cmd/moonbridge --addr "$ADDR"
) &
SERVER_PID="$!"
wait_for_server

export CODEX_HOME="$CODEX_HOME_DIR"
export MOONBRIDGE_CLIENT_API_KEY="${MOONBRIDGE_CLIENT_API_KEY:-local-dev}"

echo "Running Codex with CODEX_HOME=${CODEX_HOME_DIR}"
codex exec \
  --ignore-user-config \
  --skip-git-repo-check \
  --sandbox read-only \
  --cd /tmp \
  -m "$MODEL_ALIAS" \
  -c "model_provider=\"moonbridge\"" \
  -c "model_providers.moonbridge.name=\"Moon Bridge\"" \
  -c "model_providers.moonbridge.base_url=\"http://${ADDR}/v1\"" \
  -c "model_providers.moonbridge.env_key=\"MOONBRIDGE_CLIENT_API_KEY\"" \
  -c "model_providers.moonbridge.wire_api=\"responses\"" \
  "$PROMPT"

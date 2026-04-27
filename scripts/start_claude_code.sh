#!/usr/bin/env bash
# Start Claude Code using an already-running Moon Bridge server (CaptureAnthropic mode).
# Requires: start_moonbridge.sh to have been run first (or .moonbridge.env present).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/.moonbridge.env"
CLAUDE_CONFIG_DIR_VALUE="${ROOT_DIR}/FakeHome/ClaudeCode"
GLOBAL_CLAUDE_SETTINGS="${MOONBRIDGE_CLAUDE_SETTINGS:-"${HOME}/.claude/settings.json"}"
LOG_FILE="${ROOT_DIR}/logs/claude-code.log"
PROMPT="${1:-}"

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  echo "Do not source this script; run it directly." >&2
  return 1
fi

log() { printf '%s\n' "$*" | tee -a "$LOG_FILE"; }
log_error() { printf '%s\n' "$*" | tee -a "$LOG_FILE" >&2; }

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log_error "missing required command: $1"
    exit 1
  fi
}

require_command claude
require_command python3
mkdir -p "$CLAUDE_CONFIG_DIR_VALUE" "$(dirname "$LOG_FILE")"

# Load moonbridge connection info.
if [[ -f "$ENV_FILE" ]]; then
  source "$ENV_FILE"
else
  log_error "moonbridge env file not found at ${ENV_FILE}"
  log_error "run scripts/start_moonbridge.sh first"
  exit 1
fi

if [[ -z "${MOONBRIDGE_ADDR:-}" ]]; then
  log_error "moonbridge not configured in ${ENV_FILE}"
  exit 1
fi

# Validate mode is CaptureAnthropic.
if [[ "${MOONBRIDGE_MODE:-}" != "CaptureAnthropic" ]]; then
  log_error "moonbridge mode must be CaptureAnthropic for Claude Code, got: ${MOONBRIDGE_MODE}"
  exit 1
fi

BASE_ADDR="${MOONBRIDGE_ADDR}"
HOST="${BASE_ADDR%:*}"
PORT="${BASE_ADDR##*:}"

# Verify moonbridge is still alive.
if ! (echo > "/dev/tcp/${HOST}/${PORT}") >/dev/null 2>&1; then
  log_error "Moon Bridge not reachable on ${MOONBRIDGE_ADDR}"
  log_error "restart it with scripts/start_moonbridge.sh"
  exit 1
fi

MODEL="${MOONBRIDGE_CLAUDE_MODEL:-}"

# Generate Claude Code settings.
prepare_claude_settings() {
  local target_settings="${CLAUDE_CONFIG_DIR_VALUE}/settings.json"
  local env_file="${CLAUDE_CONFIG_DIR_VALUE}/moonbridge-env.sh"
  local base_url="http://${BASE_ADDR}"

  python3 - "$GLOBAL_CLAUDE_SETTINGS" "$target_settings" "$env_file" "$base_url" "$MODEL" <<'PY'
import json
import os
import shlex
import sys
from pathlib import Path

source_path = Path(sys.argv[1])
target_path = Path(sys.argv[2])
env_path = Path(sys.argv[3])
base_url = sys.argv[4]
model = sys.argv[5]
model_placeholders = {
    "",
    "provider-model-name",
    "replace-with-provider-model-name",
    "replace-with-real-model-name",
}

settings = {}
loaded_source = False
if source_path.exists():
    try:
        settings = json.loads(source_path.read_text())
        if not isinstance(settings, dict):
            settings = {}
        loaded_source = True
    except json.JSONDecodeError as exc:
        raise SystemExit(f"failed to parse {source_path}: {exc}") from exc

env = settings.get("env")
if not isinstance(env, dict):
    env = {}
else:
    env = {str(key): str(value) for key, value in env.items()}

env["ANTHROPIC_BASE_URL"] = base_url
env["ANTHROPIC_AUTH_TOKEN"] = "moonbridge-proxy-placeholder"
env.pop("ANTHROPIC_API_KEY", None)
env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
settings["includeCoAuthoredBy"] = False

if model and model not in model_placeholders:
    env["ANTHROPIC_MODEL"] = model
    env["ANTHROPIC_CUSTOM_MODEL_OPTION"] = model
    settings["model"] = model
elif "model" not in settings:
    env.pop("ANTHROPIC_MODEL", None)
    env.pop("ANTHROPIC_CUSTOM_MODEL_OPTION", None)

settings["env"] = env
target_path.parent.mkdir(parents=True, exist_ok=True)
target_path.write_text(json.dumps(settings, ensure_ascii=False, indent=2) + "\n")
os.chmod(target_path, 0o600)

export_keys = [
    "ANTHROPIC_AUTH_TOKEN",
    "ANTHROPIC_API_KEY",
    "ANTHROPIC_BASE_URL",
    "ANTHROPIC_MODEL",
    "ANTHROPIC_CUSTOM_MODEL_OPTION",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
]
lines = []
for key in export_keys:
    value = env.get(key)
    if value is not None:
        lines.append(f"export {key}={shlex.quote(value)}")
effective_model = env.get("ANTHROPIC_MODEL") or settings.get("model") or ""
if effective_model:
    lines.append(f"export MOONBRIDGE_EFFECTIVE_CLAUDE_MODEL={shlex.quote(str(effective_model))}")
env_path.write_text("\n".join(lines) + "\n")
os.chmod(env_path, 0o600)

if loaded_source:
    print(f"Seeded Claude Code settings from {source_path} with placeholder ANTHROPIC_AUTH_TOKEN")
else:
    print(f"No global Claude Code settings found at {source_path}; using placeholder ANTHROPIC_AUTH_TOKEN")
if model and model in model_placeholders:
    print(f"Ignoring placeholder developer.proxy.anthropic.model={model!r}; using Claude Code settings/default model")
PY

  # shellcheck source=/dev/null
  source "$env_file"
}

echo "Generating Claude Code settings..." | tee -a "$LOG_FILE"
prepare_claude_settings > >(tee -a "$LOG_FILE") 2>&1

export CLAUDE_CONFIG_DIR="$CLAUDE_CONFIG_DIR_VALUE"

log "Starting Claude Code with CLAUDE_CONFIG_DIR=${CLAUDE_CONFIG_DIR}"
log "Workspace: ${ROOT_DIR}"
log "Anthropic base URL: http://${BASE_ADDR}"
if [[ -n "${MOONBRIDGE_EFFECTIVE_CLAUDE_MODEL:-}" ]]; then
  log "Model: ${MOONBRIDGE_EFFECTIVE_CLAUDE_MODEL}"
fi

set +e
if [[ -n "$PROMPT" ]]; then
  claude "$PROMPT"
else
  claude
fi
CLAUDE_STATUS=$?
set -e

log "Claude Code exited with status ${CLAUDE_STATUS}"
exit "$CLAUDE_STATUS"

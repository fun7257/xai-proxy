# Shared defaults for example scripts. Source from each sample:
#   SCRIPT_DIR=...; # shellcheck source=...
#   . "$SCRIPT_DIR/../_common.sh"
BASE_URL="${BASE_URL:-http://127.0.0.1:8645/v1}"

# Local client API key (required). Prefer env, else $XAI_PROXY_HOME/client_key.
if [[ -z "${XAI_PROXY_CLIENT_KEY:-}" ]]; then
  KEY_HOME="${XAI_PROXY_HOME:-$HOME/.xai-proxy}"
  if [[ -f "${KEY_HOME}/client_key" ]]; then
    XAI_PROXY_CLIENT_KEY="$(tr -d '[:space:]' < "${KEY_HOME}/client_key")"
  fi
fi
if [[ -z "${XAI_PROXY_CLIENT_KEY:-}" ]]; then
  echo "Set XAI_PROXY_CLIENT_KEY or run: xai-proxy key show" >&2
  exit 1
fi
AUTH="${AUTH:-Bearer ${XAI_PROXY_CLIENT_KEY}}"
CURL_AUTH=(-H "Authorization: ${AUTH}")

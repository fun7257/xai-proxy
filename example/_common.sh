# Shared defaults for example scripts. Source from each sample:
#   SCRIPT_DIR=...; # shellcheck source=...
#   . "$SCRIPT_DIR/../_common.sh"
BASE_URL="${BASE_URL:-http://127.0.0.1:8645/v1}"
AUTH="${AUTH:-Bearer sk-local}"
CURL_AUTH=(-H "Authorization: ${AUTH}")

#!/usr/bin/env bash
# xAI-native: GET /v1/videos/{id}
# Poll until done/failed (async job from generations/edits/extensions).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

REQUEST_ID="${REQUEST_ID:-}"
if [[ -z "${REQUEST_ID}" ]]; then
  echo "Usage: REQUEST_ID=<id from submit response> bash $0" >&2
  exit 1
fi

INTERVAL="${INTERVAL:-5}"

while true; do
  BODY=$(curl -sS "${BASE_URL}/videos/${REQUEST_ID}" "${CURL_AUTH[@]}")
  echo "${BODY}"
  STATUS=$(echo "${BODY}" | python3 -c "import sys,json; d=json.load(sys.stdin); print((d.get('status') or '').lower())" 2>/dev/null || true)
  case "${STATUS}" in
    done|failed|error|expired|cancelled) break ;;
  esac
  sleep "${INTERVAL}"
done

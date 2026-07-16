#!/usr/bin/env bash
# xAI-native: GET /v1/models
# Also OpenAI full-compat.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

curl -sS "${BASE_URL}/models" \
  "${CURL_AUTH[@]}"
echo

#!/usr/bin/env bash
# xAI-native: POST /v1/completions
# Also OpenAI full-compat (legacy completions).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

curl -sS "${BASE_URL}/completions" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-4.5",
    "prompt": "Complete: The sky is",
    "max_tokens": 32,
    "temperature": 0.2
  }'
echo

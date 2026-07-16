#!/usr/bin/env bash
# xAI-native: POST /v1/responses
# Also OpenAI full-compat (Responses-style).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

curl -sS "${BASE_URL}/responses" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-4.5",
    "input": [
      {
        "role": "user",
        "content": "In one sentence, what is a reverse proxy?"
      }
    ],
    "store": false
  }'
echo

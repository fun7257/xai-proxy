#!/usr/bin/env bash
# xAI-native: POST /v1/chat/completions
# Also OpenAI full-compat (same path + body shape).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

curl -sS "${BASE_URL}/chat/completions" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-4.5",
    "messages": [
      {"role": "system", "content": "You are a concise assistant."},
      {"role": "user", "content": "Say hello in one short sentence."}
    ],
    "temperature": 0.3
  }'
echo

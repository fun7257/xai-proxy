#!/usr/bin/env bash
# xAI-native: POST /v1/embeddings
# Also OpenAI full-compat.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

# Model id may vary by account; list models first if this 404s.
curl -sS "${BASE_URL}/embeddings" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-embedding",
    "input": "hello from xai-proxy embeddings example"
  }'
echo

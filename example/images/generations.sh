#!/usr/bin/env bash
# xAI-native: POST /v1/images/generations
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

curl -sS "${BASE_URL}/images/generations" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-image",
    "prompt": "a red panda coding in a cyber cafe, cinematic lighting",
    "aspect_ratio": "16:9",
    "resolution": "1k"
  }'
echo

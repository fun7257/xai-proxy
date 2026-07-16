#!/usr/bin/env bash
# xAI-native: POST /v1/videos/edits
# Requires a public HTTPS video URL from a prior Imagine result.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

VIDEO_URL="${VIDEO_URL:-https://example.com/your-public-video.mp4}"

curl -sS "${BASE_URL}/videos/edits" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"grok-imagine-video\",
    \"prompt\": \"Add soft golden-hour color grade and slight film grain\",
    \"video\": \"${VIDEO_URL}\"
  }"
echo

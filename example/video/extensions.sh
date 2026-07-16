#!/usr/bin/env bash
# xAI-native: POST /v1/videos/extensions
# Extend an existing public video URL.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

VIDEO_URL="${VIDEO_URL:-https://example.com/your-public-video.mp4}"

curl -sS "${BASE_URL}/videos/extensions" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"grok-imagine-video\",
    \"prompt\": \"Continue the camera motion smoothly\",
    \"video\": \"${VIDEO_URL}\",
    \"duration\": 6
  }"
echo

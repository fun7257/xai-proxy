#!/usr/bin/env bash
# xAI-native: POST /v1/videos/generations (async submit)
# Response typically includes request_id — then poll with status_poll.sh
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

curl -sS "${BASE_URL}/videos/generations" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video",
    "prompt": "Cinematic drone shot of ocean waves at golden hour, gentle camera push-in",
    "duration": 6,
    "aspect_ratio": "16:9",
    "resolution": "720p"
  }'
echo

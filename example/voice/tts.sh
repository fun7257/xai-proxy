#!/usr/bin/env bash
# xAI-native: POST /v1/tts
# NOT OpenAI /v1/audio/speech — use this path only.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

OUT="${OUT:-./speech.mp3}"

curl -sS "${BASE_URL}/tts" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "Hello from Grok TTS via xai-proxy.",
    "voice_id": "Ara",
    "language": "en"
  }' \
  --output "${OUT}"

echo "wrote ${OUT}"

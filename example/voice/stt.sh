#!/usr/bin/env bash
# xAI-native: POST /v1/stt (multipart)
# NOT OpenAI /v1/audio/transcriptions — use this path only.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

# Point at a real local audio file before running.
AUDIO_FILE="${AUDIO_FILE:-./audio.wav}"

if [[ ! -f "${AUDIO_FILE}" ]]; then
  echo "Set AUDIO_FILE to an existing audio file (got: ${AUDIO_FILE})" >&2
  echo "Example: AUDIO_FILE=/path/to/clip.wav bash $0" >&2
  exit 1
fi

curl -sS "${BASE_URL}/stt" \
  "${CURL_AUTH[@]}" \
  -F "file=@${AUDIO_FILE}" \
  -F "language=en" \
  -F "format=true"
echo

#!/usr/bin/env bash
# xAI-native: POST /v1/images/edits
# Body is JSON (public HTTPS URL or data URI) — not OpenAI multipart images.edit().
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../_common.sh
. "$SCRIPT_DIR/../_common.sh"

# Replace IMAGE_URL with a public image URL (e.g. from a prior generations result).
IMAGE_URL="${IMAGE_URL:-https://example.com/your-public-image.png}"

curl -sS "${BASE_URL}/images/edits" \
  "${CURL_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"grok-imagine-image-quality\",
    \"prompt\": \"make the sky sunset orange\",
    \"image\": \"${IMAGE_URL}\"
  }"
echo

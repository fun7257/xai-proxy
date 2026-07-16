# xai-proxy request examples

All samples hit the **local proxy** at `http://127.0.0.1:8645/v1/...`.

Prerequisites:

```bash
./xai-proxy login
./xai-proxy serve
```

**Policy:** every path below is an **xAI-native** route. Chat/text paths also work with
stock OpenAI SDKs (full path+body compat). There are **no** `/audio/*` examples — use
`/tts` and `/stt`.

## Index (full path coverage)

| Category | Path | File |
|----------|------|------|
| chat | `POST /v1/chat/completions` | [chat/chat_completions.sh](chat/chat_completions.sh) |
| chat | `POST /v1/responses` | [chat/responses.sh](chat/responses.sh) |
| chat | `POST /v1/completions` | [chat/completions.sh](chat/completions.sh) |
| chat | `POST /v1/embeddings` | [chat/embeddings.sh](chat/embeddings.sh) |
| chat | `GET  /v1/models` | [chat/models.sh](chat/models.sh) |
| images | `POST /v1/images/generations` | [images/generations.sh](images/generations.sh) |
| images | `POST /v1/images/edits` | [images/edits.sh](images/edits.sh) |
| voice | `POST /v1/tts` | [voice/tts.sh](voice/tts.sh) |
| voice | `POST /v1/stt` | [voice/stt.sh](voice/stt.sh) |
| video | `POST /v1/videos/generations` | [video/generations.sh](video/generations.sh) |
| video | `POST /v1/videos/edits` | [video/edits.sh](video/edits.sh) |
| video | `POST /v1/videos/extensions` | [video/extensions.sh](video/extensions.sh) |
| video | `GET  /v1/videos/{id}` | [video/status_poll.sh](video/status_poll.sh) |

Common env (optional):

```bash
export BASE_URL="${BASE_URL:-http://127.0.0.1:8645/v1}"
export AUTH="${AUTH:-Bearer sk-local}"   # ignored by proxy; OAuth is attached server-side
```

Run any sample:

```bash
bash example/chat/chat_completions.sh
bash example/video/generations.sh
# then:
REQUEST_ID=... bash example/video/status_poll.sh
```

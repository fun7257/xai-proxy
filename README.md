# xai-proxy

Local reverse proxy for **xAI** (Grok OAuth). Log in once; any client on
loopback can call chat **and multimodal** APIs without managing an API key.

The proxy attaches your OAuth bearer and **pass-through** forwards to
`https://api.x.ai/v1/*`.

## Requirements

- Go **1.26.5**
- SuperGrok or X Premium+ (xAI OAuth API access; some modalities may still be tier-gated upstream)

## Install

```bash
cd xai-proxy
make build
# or: go build -o xai-proxy ./cmd/xai-proxy
```

### Docker (recommended for always-on)

```bash
docker compose build
docker compose run --rm xai-proxy login --no-browser   # open printed URL
docker compose up -d
curl -s http://127.0.0.1:8645/health
```

Host port is published as **`127.0.0.1:8645` only**. Tokens live in volume
`xai-proxy-data`. Full guide: [docs/DOCKER.md](docs/DOCKER.md).

## Usage

```bash
./xai-proxy login
./xai-proxy serve   # http://127.0.0.1:8645
```

Copy-paste request samples for **every allowed path** (by category): see **[example/](example/)**.

| Client setting | Value |
|----------------|--------|
| Base URL | `http://127.0.0.1:8645/v1` |
| API Key | anything (ignored) |

## Path policy

**All forwarded routes are xAI-native.** Some chat/text routes *also* match
OpenAI path + body shape (full compat) so OpenAI SDKs work unchanged. No shims
for partial matches (e.g. no `/audio/speech` → `/tts`).

### Supported xAI-native paths (`/v1/...`)

| Path | Capability | Also OpenAI full-compat |
|------|------------|-------------------------|
| `POST /chat/completions` | Chat (e.g. `grok-4.5`) | yes |
| `POST /responses` | Responses API | yes |
| `POST /completions` | Completions | yes |
| `POST /embeddings` | Embeddings | yes |
| `GET  /models` | Model list | yes |
| `POST /images/generations` | Image gen | no |
| `POST /images/edits` | Image edit (JSON) | no |
| `POST /tts` | Text-to-speech | no |
| `POST /stt` | Speech-to-text (multipart) | no |
| `POST /videos/generations` | Video submit | no |
| `POST /videos/edits` | Video edit | no |
| `POST /videos/extensions` | Video extend | no |
| `GET  /videos/{id}` | Video job status | no |

Rejected (no shim): `/audio/speech`, `/audio/transcriptions`, `/audio/translations` → use `/tts` / `/stt`.

Body size limit: **100 MiB** (media / data-URI / STT upload).

### Examples

```bash
# Chat (xAI native; also works with OpenAI SDK)
curl -s http://127.0.0.1:8645/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}]}'

# Image
curl -s http://127.0.0.1:8645/v1/images/generations \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-image","prompt":"a red panda coding"}'

# TTS (not /audio/speech)
curl -s http://127.0.0.1:8645/v1/tts \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from Grok","voice_id":"Ara","language":"en"}' \
  -o speech.mp3

# STT (not /audio/transcriptions)
curl -s http://127.0.0.1:8645/v1/stt \
  -F 'file=@./audio.wav' \
  -F 'language=en'

# Video submit
curl -s http://127.0.0.1:8645/v1/videos/generations \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-video","prompt":"waves on a beach"}'
```

## Storage

| Path | Purpose |
|------|---------|
| `~/.xai-proxy/tokens.json` | OAuth tokens (`0600`) |
| `XAI_PROXY_HOME` | Override config directory |

## Security

- Default bind: **127.0.0.1** (no client auth)
- Non-loopback requires `--i-understand-no-client-auth`
- See [docs/SECURITY.md](docs/SECURITY.md) and [AGENTS.md](AGENTS.md)

## Commands

```text
xai-proxy login [--no-browser]
xai-proxy serve [--host 127.0.0.1] [--port 8645]
xai-proxy status
xai-proxy logout
xai-proxy version
```

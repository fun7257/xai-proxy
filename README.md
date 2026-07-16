# xai-proxy

**Local developer tool** — a single-operator reverse proxy that logs you into
xAI (Grok) via OAuth once, then lets any **local** OpenAI-compatible client call
xAI’s native `/v1/*` APIs (chat **and** multimodal) without managing an API key.

The proxy attaches your OAuth bearer and **pass-through** forwards to
`https://api.x.ai/v1/*`.

> **Not affiliated with xAI.** Unofficial, community-maintained software.  
> **For your machine only** — not a multi-tenant or public API gateway.  
> License: [MIT](LICENSE). Security model: [SECURITY.md](SECURITY.md).

## Important: security & scope

| Do | Don’t |
|----|--------|
| Bind **127.0.0.1** (default) | Expose the port to the public internet |
| Use Docker with host map `127.0.0.1:8645` only | Publish `0.0.0.0:8645` on a shared host |
| Treat `~/.xai-proxy/tokens.json` as a secret | Commit tokens, `.env`, or volume dumps |
| Put auth **in front** if you must share on a network | Assume the proxy authenticates clients |

**No client authentication.** Any process that can reach the listen socket can
spend **your** SuperGrok / OAuth quota (chat, images, TTS/STT, video).

OAuth uses a **public** device-code client id (same class of flow as common
Grok/Hermes-style CLI tools). xAI may change allowlists or terms at any time;
use at your own risk.

Full threat model: [docs/SECURITY.md](docs/SECURITY.md).

## What this is for

- Point **Cursor / OpenAI SDKs / curl** at a local base URL during development
- One OAuth login, automatic token refresh, native xAI paths for all modalities
- Optional Docker for always-on local use

## What this is not

- An official xAI product or supported integration
- A secure shared proxy for a team or the internet
- A full OpenAI Audio API shim (`/audio/speech` etc. are **not** mapped)

## Requirements

- Go **1.26.5** (or Docker)
- SuperGrok or X Premium+ (OAuth API access; some modalities may be tier-gated upstream)

## Install

```bash
cd xai-proxy
make build
# or: go build -o xai-proxy ./cmd/xai-proxy
```

### Container (local always-on, Apple Container on macOS)

Dockerfile is OCI-compatible; no Compose required. On macOS prefer `container`:

```bash
container system start
container builder start   # first time / if builder down

container build -t xai-proxy:local -f Dockerfile \
  --build-arg GO_VERSION=1.26.5 --build-arg VERSION=0.1.0 .

mkdir -p "$HOME/.xai-proxy-container" && chmod 700 "$HOME/.xai-proxy-container"
container run --rm -it --volume "$HOME/.xai-proxy-container:/data" \
  --env XAI_PROXY_HOME=/data xai-proxy:local login --no-browser

container run -d --name xai-proxy --publish 8645:8645 \
  --volume "$HOME/.xai-proxy-container:/data" --env XAI_PROXY_HOME=/data \
  xai-proxy:local

curl -s http://127.0.0.1:8645/health
```

Full guide: [docs/DOCKER.md](docs/DOCKER.md).

## Usage

```bash
./xai-proxy login
./xai-proxy serve   # http://127.0.0.1:8645
```

Copy-paste request samples for **every allowed path** (by category): **[example/](example/)**.

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
| `~/.xai-proxy/tokens.json` | OAuth tokens (`0600`) — **secret** |
| `XAI_PROXY_HOME` | Override config directory |

Never commit tokens or Docker volume contents.

## Commands

```text
xai-proxy login [--no-browser]
xai-proxy serve [--host 127.0.0.1] [--port 8645]
xai-proxy status
xai-proxy logout
xai-proxy version
```

## Docs

| Doc | Content |
|-----|---------|
| [SECURITY.md](SECURITY.md) | Scope, intended use, how to report issues |
| [docs/SECURITY.md](docs/SECURITY.md) | Threat model & controls |
| [docs/DOCKER.md](docs/DOCKER.md) | Container deployment |
| [docs/DESIGN.md](docs/DESIGN.md) | Architecture sketch |
| [AGENTS.md](AGENTS.md) | Contributor / agent hard constraints |
| [example/](example/) | Full-path curl samples |

## Disclaimer

This software is provided **as is**, under the [MIT License](LICENSE), without
warranty. You are responsible for complying with [xAI](https://x.ai) terms of
service, subscription rules, and any applicable law. The authors are not
responsible for account bans, quota use, or data you send to upstream APIs.

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
| Use the **local client API key** on every `/v1/*` call | Call `/v1` without `Authorization` |
| Treat `tokens.json` **and** `client_key` as secrets | Commit keys, `.env`, or volume dumps |
| Keep client key off shared hosts | Share the key like a password |

**Local client auth is required** on `/v1/*`: `Authorization: Bearer <client_key>`.  
`/health` and `/ready` stay open for probes. Upstream OAuth is separate (attached by the proxy).

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

### Container (local always-on)

On **macOS**, use Apple Container (`container` CLI):

```bash
container system start && container builder start

container build -t xai-proxy:local -f Dockerfile \
  --build-arg GO_VERSION=1.26.5 --build-arg VERSION=0.1.0 .

mkdir -p "$HOME/.xai-proxy-container" && chmod 700 "$HOME/.xai-proxy-container"
# Default CMD is `start`: login (--no-browser) if needed, then serve.
# First run: watch logs for the device URL, approve in a browser.
container run -d --name xai-proxy --publish 8645:8645 \
  --volume "$HOME/.xai-proxy-container:/data" --env XAI_PROXY_HOME=/data \
  xai-proxy:local
container logs -f xai-proxy

curl -s http://127.0.0.1:8645/health
```

- **Apple Container 完整指南**：[docs/CONTAINER.md](docs/CONTAINER.md)  
- Docker Engine 可选：[docs/DOCKER.md](docs/DOCKER.md)

## Usage

```bash
./xai-proxy login
./xai-proxy serve   # http://127.0.0.1:8645
```

Copy-paste request samples for **every allowed path** (by category): **[example/](example/)**.

| Client setting | Value |
|----------------|--------|
| Base URL | `http://127.0.0.1:8645/v1` |
| API Key | **local client key** (`xai-proxy key show`) |

```bash
export XAI_PROXY_CLIENT_KEY="$(xai-proxy key show 2>/dev/null | head -1)"
# or: cat ~/.xai-proxy/client_key
```

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
KEY=$(xai-proxy key show 2>/dev/null | head -1)

# Chat (xAI native; also works with OpenAI SDK)
curl -s http://127.0.0.1:8645/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}]}'

# Image
curl -s http://127.0.0.1:8645/v1/images/generations \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-image","prompt":"a red panda coding"}'

# TTS (not /audio/speech)
curl -s http://127.0.0.1:8645/v1/tts \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from Grok","voice_id":"Ara","language":"en"}' \
  -o speech.mp3

# STT (not /audio/transcriptions)
curl -s http://127.0.0.1:8645/v1/stt \
  -H "Authorization: Bearer $KEY" \
  -F 'file=@./audio.wav' \
  -F 'language=en'

# Video submit
curl -s http://127.0.0.1:8645/v1/videos/generations \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-video","prompt":"waves on a beach"}'
```

## Storage

| Path | Purpose |
|------|---------|
| `~/.xai-proxy/tokens.json` | OAuth tokens (`0600`) — **secret** |
| `~/.xai-proxy/client_key` | Local client API key (`0600`) — **secret** |
| `XAI_PROXY_HOME` | Override config directory |
| `XAI_PROXY_CLIENT_KEY` | Optional env override for client key |

Never commit tokens, client keys, or volume contents.

## Outbound proxy (HTTP / SOCKS)

All **egress** to xAI (OAuth discovery, device login, token refresh, API forward)
shares one proxy policy. This is **not** an inbound client proxy.

| Source | Examples |
|--------|----------|
| CLI (highest) | `--proxy socks5://127.0.0.1:1080` or `--proxy http://127.0.0.1:7890` |
| Env | `XAI_PROXY_OUTBOUND`, then `ALL_PROXY`, `HTTPS_PROXY`, `HTTP_PROXY` |
| Bypass | `NO_PROXY` / `no_proxy` |
| Direct | unset all of the above |

Schemes: `http://`, `https://`, `socks5://`, `socks5h://` (`socks://` → socks5).

```bash
# SOCKS5 (common local clients)
export ALL_PROXY=socks5://127.0.0.1:1080
./xai-proxy login --no-browser
./xai-proxy serve

# Or explicit flag (wins over env)
./xai-proxy --proxy http://127.0.0.1:7890 serve
./xai-proxy serve --proxy socks5h://127.0.0.1:1080
```

## Commands

```text
xai-proxy start   # login if needed (--no-browser), then serve (container default)
xai-proxy login [--no-browser]
xai-proxy serve
xai-proxy status | logout | version
```

## Docs

| Doc | Content |
|-----|---------|
| [SECURITY.md](SECURITY.md) | Scope, intended use, how to report issues |
| [docs/SECURITY.md](docs/SECURITY.md) | Threat model & controls |
| [docs/CONTAINER.md](docs/CONTAINER.md) | **Apple Container** 部署（推荐 macOS） |
| [docs/DOCKER.md](docs/DOCKER.md) | Docker Engine 简要命令（可选） |
| [docs/DESIGN.md](docs/DESIGN.md) | Architecture sketch |
| [AGENTS.md](AGENTS.md) | Contributor / agent hard constraints |
| [example/](example/) | Full-path curl samples |

## Disclaimer

This software is provided **as is**, under the [MIT License](LICENSE), without
warranty. You are responsible for complying with [xAI](https://x.ai) terms of
service, subscription rules, and any applicable law. The authors are not
responsible for account bans, quota use, or data you send to upstream APIs.

# xai-proxy

**Local developer tool** — a single-operator reverse proxy that logs you into
xAI (Grok) via OAuth once, then lets any **local** OpenAI-compatible client call
xAI’s native `/v1/*` APIs (chat **and** multimodal) without managing an API key.

The proxy attaches your OAuth bearer and **pass-through** forwards to
`https://api.x.ai/v1/*`.

> **Not affiliated with xAI.** Unofficial, community-maintained software.  
> **For your machine only** — not a multi-tenant or public API gateway.  
> License: [MIT](LICENSE). Security model: [docs/SECURITY.md](docs/SECURITY.md).  
> Chinese: [README_zh.md](README_zh.md).

## Important: security & scope

| Do | Don’t |
|----|--------|
| Bind **127.0.0.1** (default) | Expose the port to the public internet |
| Use the **local client API key** on every `/v1/*` call | Call `/v1` without `Authorization` |
| Treat OAuth tokens **and** the generated client key as secrets | Commit keys, `.env`, or volume dumps |
| Keep client key off shared hosts | Share the key like a password |

**Local client auth is required** on `/v1/*`: `Authorization: Bearer <client_key>`.  
`/health` and `/ready` stay open for probes. Upstream OAuth is separate (attached by the proxy).

OAuth uses a **public** device-code client id. xAI may change allowlists or
terms at any time; use at your own risk.

Full threat model: [docs/SECURITY.md](docs/SECURITY.md).

## What this is for

- Point **Cursor / OpenAI SDKs / curl** at a local base URL during development
- One OAuth login, automatic token refresh, native xAI paths for all modalities

## What this is not

- An official xAI product or supported integration
- A secure shared proxy for a team or the internet
- A full OpenAI Audio API shim (`/audio/speech` etc. are **not** mapped)

## Requirements

- Go **1.26.5**
- SuperGrok or X Premium+ (OAuth API access; some modalities may be tier-gated upstream)

## Install

```bash
cd xai-proxy
make build
# or: go build -o xai-proxy ./cmd/xai-proxy
```

## Usage

```bash
# Mint a local client API key (printed ONCE to stdout — save it; overwrites previous)
KEY=$(./xai-proxy generate)

./xai-proxy login
./xai-proxy serve   # http://127.0.0.1:7257
```

| Client setting | Value |
|----------------|--------|
| Base URL | `http://127.0.0.1:7257/v1` |
| API Key | **local client key** from `xai-proxy generate` (shown once) |

There is **no** `key show`. If you lose the key, run `generate` again (old key stops working).

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
# KEY from: xai-proxy generate  (shown once)

# Chat (xAI native; also works with OpenAI SDK)
curl -s http://127.0.0.1:7257/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}]}'

# Image
curl -s http://127.0.0.1:7257/v1/images/generations \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-image","prompt":"a red panda coding"}'

# TTS (not /audio/speech)
curl -s http://127.0.0.1:7257/v1/tts \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from Grok","voice_id":"Ara","language":"en"}' \
  -o speech.mp3

# STT (not /audio/transcriptions)
curl -s http://127.0.0.1:7257/v1/stt \
  -H "Authorization: Bearer $KEY" \
  -F 'file=@./audio.wav' \
  -F 'language=en'

# Video submit
curl -s http://127.0.0.1:7257/v1/videos/generations \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-video","prompt":"waves on a beach"}'
```

## Storage

| Path | Purpose |
|------|---------|
| `~/.xai-proxy/tokens.json` | OAuth tokens (`0600`) — **secret** |
| `~/.xai-proxy/client_key` | Salted **SHA-256** hash of the client key only (`0600`); each `generate` overwrites |

Default directory is `~/.xai-proxy` (override with `XAI_PROXY_HOME`).  
The plaintext client key is shown **only** by `generate`. The on-disk file is a
verifier (`v1$sha256$…`) and cannot recover the secret.
Never commit tokens, client keys, or volume contents.

## Environment variables

### Project

| Variable | Purpose |
|----------|---------|
| `XAI_PROXY_HOME` | Config directory for `tokens.json` and `client_key` (default `~/.xai-proxy`) |
| `XAI_PROXY_OUTBOUND` | Outbound HTTP/SOCKS proxy for OAuth + API (preferred over standard proxy env vars) |
| `XAI_BASE_URL` | Optional inference base written at login (default `https://api.x.ai/v1`; must be HTTPS on `*.x.ai`) |

There is **no** env override for the local client API key (use `xai-proxy generate` only).

### Outbound proxy (standard)

Applies to **egress** only (OAuth discovery, device login, refresh, API forward) — not inbound clients.

| Variable | Role |
|----------|------|
| `XAI_PROXY_OUTBOUND` | Project-specific proxy URL (highest among env vars) |
| `ALL_PROXY` / `all_proxy` | Unified proxy (often SOCKS5) |
| `HTTPS_PROXY` / `https_proxy` | HTTPS proxy |
| `HTTP_PROXY` / `http_proxy` | HTTP proxy |
| `NO_PROXY` / `no_proxy` | Hosts that bypass the proxy |

**Priority** (high → low): CLI `--proxy` → `XAI_PROXY_OUTBOUND` → `ALL_PROXY` → `HTTPS_PROXY` → `HTTP_PROXY` → direct.

Schemes: `http://`, `https://`, `socks5://`, `socks5h://` (`socks://` → socks5).

```bash
# Config dir (optional)
export XAI_PROXY_HOME="$HOME/.xai-proxy"

# SOCKS5 egress (common local clients)
export ALL_PROXY=socks5://127.0.0.1:1080
# or: export XAI_PROXY_OUTBOUND=socks5://127.0.0.1:1080

./xai-proxy login --no-browser
./xai-proxy serve

# Explicit flag wins over env
./xai-proxy --proxy http://127.0.0.1:7890 serve
./xai-proxy serve --proxy socks5h://127.0.0.1:1080
```

## Commands

```text
xai-proxy generate   # mint client key (overwrite; print once)
xai-proxy login [--no-browser]
xai-proxy serve
xai-proxy status | logout | version
```

## Docs

| Doc | Content |
|-----|---------|
| [docs/SECURITY.md](docs/SECURITY.md) | Scope, threat model, reporting |
| [docs/DESIGN.md](docs/DESIGN.md) | Architecture, paths, OAuth, CLI |
| [AGENTS.md](AGENTS.md) | Dev style & security guidelines |

Chinese translations use the `*_zh.md` suffix (e.g. [README_zh.md](README_zh.md),
[docs/DESIGN_zh.md](docs/DESIGN_zh.md), [docs/SECURITY_zh.md](docs/SECURITY_zh.md)).
`AGENTS.md` is English-only. Docs and code comments are English by default.

## Disclaimer

This software is provided **as is**, under the [MIT License](LICENSE), without
warranty. You are responsible for complying with [xAI](https://x.ai) terms of
service, subscription rules, and any applicable law. The authors are not
responsible for account bans, quota use, or data you send to upstream APIs.

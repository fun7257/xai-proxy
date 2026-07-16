# Design

Architecture and product contracts. Development style and security coding rules:
root [AGENTS.md](../AGENTS.md). Threat model: [SECURITY.md](SECURITY.md).  
Chinese: [DESIGN_zh.md](DESIGN_zh.md).

## What this is

- A **local developer tool** (single operator; not SaaS; not an official xAI product)
- Local xAI OAuth (device code) login + a local **xAI-native `/v1/*` reverse proxy** (chat + multimodal)
- Open-source positioning: root `README.md`, `LICENSE` (MIT); security: [SECURITY.md](SECURITY.md)

## Dual-layer authentication

| Layer | Credential | Direction | Storage |
|-------|------------|-----------|---------|
| Local client | `sk-xai-…` (plaintext only at generate) | client → proxy | Disk: salted SHA-256 in `client_key` (latest only) |
| Upstream OAuth | access / refresh | proxy → xAI | `~/.xai-proxy/tokens.json` |

- `/v1/*`: hash presented key → constant-time compare to stored verifier → strip client auth headers → attach OAuth Bearer
- `/health`, `/ready`: open (probes)
- CLI: `xai-proxy generate` (overwrites previous verifier; plaintext printed **once** on stdout; never re-stored as plaintext)
- Verifier format: `v1$sha256$<salt_hex>$<hash_hex>` (stdlib `crypto/sha256`)

## Layers

1. **Auth** (`internal/auth`) — device login, OIDC discovery, refresh, JWT skew, host pin  
2. **Store** (`internal/store`) — atomic read/write of `tokens.json` / `client_key` + flock  
3. **Credential manager** (`internal/credential`) — `GetBearer` / `ForceRefresh` / `Status`  
4. **Proxy** (`internal/proxy`) — `net/http` pass-through + path allowlist + client auth  
5. **Outbound** (`internal/outbound`) — HTTP/SOCKS egress proxy policy  
6. **CLI** (`internal/cli`) — `generate` / `login` / `serve` / `status` / …

## Path policy

### Principles

1. **Every forwarded route is an xAI-native path** (chat / image / voice / video treated the same)
2. A **subset** shares OpenAI path + body shape → **additionally** usable as OpenAI SDK `base_url` (full compat)
3. **If full path+body compat is impossible → no shim**; clients call the xAI-native path

### Supported xAI-native paths (under `/v1`)

| Path | Capability | Also OpenAI full-compat |
|------|------------|-------------------------|
| `/chat/completions` | Chat | yes |
| `/responses` | Responses API | yes |
| `/completions` | Completions | yes |
| `/embeddings` | Embeddings | yes |
| `/models` | Model list | yes |
| `/images/generations` | Image gen | no (xAI extension fields) |
| `/images/edits` | Image edit (JSON) | no |
| `/tts` | Text-to-speech | no |
| `/stt` | Speech-to-text (multipart) | no |
| `/videos/generations` | Video submit | no |
| `/videos/edits` | Video edit | no |
| `/videos/extensions` | Video extend | no |
| `/videos/{id}` | Video job status | no |

- Exact allow: fixed paths in the table above  
- Pattern allow: `/videos/{id}` for async status  
- Default body limit: **100 MiB**

### Explicitly rejected OpenAI-only audio paths (no shim)

- `/audio/speech` → use native `/tts`
- `/audio/transcriptions` → use native `/stt`
- `/audio/translations` → not mapped

Implementation: `internal/proxy/allowlist.go`.

## OAuth contract

| Item | Value |
|------|-------|
| Client ID | `b1a00492-073a-47ea-816f-4c329264a828` (public device client) |
| Scope | `openid profile email offline_access grok-cli:access api:access` |
| Device | `POST https://auth.x.ai/oauth2/device/code` |
| Token | discovery `token_endpoint` (typically `https://auth.x.ai/oauth2/token`) |
| API | `https://api.x.ai/v1` |

Refresh: single-use, atomic write-back; **403** = tier/entitlement denial; terminal errors such as `invalid_grant` → quarantine / re-login.

## CLI

```text
xai-proxy generate                          # mint client key (overwrite; show once)
xai-proxy [--proxy URL] login   [--no-browser] [--proxy URL]
xai-proxy [--proxy URL] serve   [--host ...] [--port ...] [--proxy URL]
xai-proxy status | logout | version
```

### Outbound proxy (shared by OAuth + API)

| Source | Notes |
|--------|-------|
| CLI | `--proxy socks5://…` / `http://…` (highest priority) |
| Env | `XAI_PROXY_OUTBOUND`, then `ALL_PROXY` / `HTTPS_PROXY` / `HTTP_PROXY` |
| Bypass | `NO_PROXY` / `no_proxy` |

Implementation: `internal/outbound`.

### Config directory

| Path / variable | Purpose |
|-----------------|---------|
| `~/.xai-proxy/tokens.json` | OAuth tokens (`0600`) |
| `~/.xai-proxy/client_key` | Salted SHA-256 verifier only (`0600`); each `generate` overwrites |
| `XAI_PROXY_HOME` | Override config directory |

## Stack

- Go **1.26.5**
- HTTP: **`net/http` only** (no third-party routers)
- Default zero third-party; outbound SOCKS: `golang.org/x/net`

## Explicitly out of scope (product)

- Multi-tenant SaaS / public identity systems (local shared client key only, not a full user system)
- Impersonating an official xAI product
- Twitter official OAuth API
- Fake OpenAI→xAI TTS/STT/video compatibility shims
- WebSocket Voice realtime gateway (not a current goal)

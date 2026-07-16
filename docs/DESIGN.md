# Design summary

See [AGENTS.md](../AGENTS.md) for hard constraints.

## Layers

1. **Auth** — device code, OIDC discovery, refresh, host pin  
2. **Credential manager** — `GetBearer` / `ForceRefresh` / file lock / singleflight  
3. **Proxy** — `net/http` pass-through forwarder + **xAI-native path policy**  

## Path policy

- Exact allow: chat, images, `/tts`, `/stt`, video actions  
- Pattern allow: `/videos/{id}` for async status  
- **No** OpenAI `/audio/*` shims  

## Native paths + optional OpenAI full-compat

All allowed routes are **xAI-native**. A subset also works as OpenAI full-compat
(same path + body) so stock OpenAI SDKs can target the proxy for chat/text.

| Paths | OpenAI full-compat (extra) |
|-------|----------------------------|
| `/chat/completions`, `/responses`, `/completions`, `/embeddings`, `/models` | yes |
| `/images/*`, `/tts`, `/stt`, `/videos/*` | no |

## Stack

- Go 1.26.5  
- stdlib only (`net/http`)  
- Default body limit 100 MiB  

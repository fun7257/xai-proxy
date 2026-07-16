# Security model (operator guide)

## Product positioning

**xai-proxy is a local developer tool** for a single human operator on a
workstation (or a Docker container bound to host loopback).

It is **not** multi-tenant SaaS, **not** an xAI official product, and **not**
safe to expose as a public API without additional authentication in front.

## Threat model

Anyone who can connect to the listen socket can:

- Invoke chat, image, TTS/STT, and video endpoints on **your** OAuth account
- Spend SuperGrok / subscription quota and trigger billable or rate-limited usage

Attack surface is therefore **network reachability of the proxy port**, not a
missing “client API key” feature — client auth is intentionally absent for local DX.

## Controls

| Control | Default |
|---------|---------|
| Listen address | `127.0.0.1` only (CLI) |
| Client authentication | **None** (by design) |
| Token file | `~/.xai-proxy/tokens.json` mode `0600`, dir `0700` |
| Upstream transport | HTTPS only |
| Host pin | discovery / token / inference must be `*.x.ai` |
| Logging | never logs access or refresh tokens |
| Path allowlist | only known xAI-native `/v1` families |
| Body size | default max **100 MiB** (media); reduces exposure if you lower it |

## Non-loopback

Binding `0.0.0.0` (or any non-loopback address) requires:

```bash
xai-proxy serve --host 0.0.0.0 --i-understand-no-client-auth
```

Do this only behind a trusted network, VPN, or reverse proxy **with its own auth**.

### Docker

The container process binds `0.0.0.0:8645` (Docker NAT requirement) **with**
`--i-understand-no-client-auth`. Compose defaults to publishing only
`127.0.0.1:8645:8645` on the host. Do not change that to `0.0.0.0:8645` on a
shared machine without an outer auth layer. Token volume `xai-proxy-data` is
secret material — restrict Docker volume access.

## OAuth credentials

- Uses a **public** OAuth device-code client (no client secret in the app).
- Refresh tokens are **single-use** (rotated). Concurrent processes use a file lock.
- Terminal refresh failures (`invalid_grant`) quarantine local tokens and require `login` again.
- HTTP **403** on refresh is treated as **tier/entitlement denial**, not expiry.
- Upstream policy (allowed clients, scopes, tiers) can change without notice.

## Header forwarding

Client headers other than hop-by-hop fields and `Authorization` are forwarded
to `api.x.ai`. Prefer simple API clients over a full browser pointed at the
proxy, to avoid sending unnecessary cookies.

## Reporting issues

See the repository root [SECURITY.md](../SECURITY.md).

# Security policy

## What this project is

**xai-proxy** is a **local developer tool**: a single-operator, machine-local
reverse proxy that attaches your xAI OAuth credentials to outbound API calls.

It is **not**:

- an xAI / Grok / SuperGrok official product
- a multi-tenant or public SaaS gateway
- a substitute for putting authentication in front of a network-exposed service

## Intended deployment

| Intended | Not intended |
|----------|----------------|
| `127.0.0.1` / localhost on your workstation | Public internet without an outer auth layer |
| Docker published as `127.0.0.1:8645` only | Shared LAN bind with no extra auth |
| One human operator, one OAuth login | Untrusted multi-user access to the proxy port |

**There is no client authentication** on the proxy. Anyone who can open a TCP
connection to the listen address can use **your** subscription quota (chat,
image, TTS/STT, video, etc.).

## Reporting vulnerabilities

If you believe you found a security issue **in this software** (not in xAI’s
upstream API):

1. Prefer a private report (GitHub Security Advisory / email to maintainers if published).
2. Do **not** open a public issue that includes live tokens, refresh tokens, or
   full request captures containing secrets.
3. Include: affected version/commit, reproduction steps, impact assessment.

We aim to acknowledge valid reports in a reasonable time. There is no formal
bug bounty.

## Out of scope (please don’t report as product bugs)

- “Proxy has no API key for clients” — **by design** for local use
- Abuse after the operator binds `0.0.0.0` or exposes Docker to the world
- Upstream xAI 403 / tier / rate limits
- Using a third-party OAuth public client subject to xAI policy changes

## Operator hardening

See [docs/SECURITY.md](docs/SECURITY.md) for threat model, token storage, and
host pinning. For running in containers on macOS, see
[docs/CONTAINER.md](docs/CONTAINER.md).

# Security

Operator security notes, threat model, and vulnerability reporting.  
Development style and coding-time security rules: root [AGENTS.md](../AGENTS.md).  
Architecture and dual-layer auth: [DESIGN.md](DESIGN.md).  
Chinese: [SECURITY_zh.md](SECURITY_zh.md).

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
| One human operator, one OAuth login | Shared LAN / untrusted multi-user access to the proxy port |

**Local client authentication is required** for `/v1/*` (`Authorization: Bearer
<client_key>`). Anyone with the key **and** network reachability can use
**your** subscription quota. `/health` and `/ready` remain open for probes.

## Threat model

Anyone who can connect to the listen socket **and** present a valid client key can:

- Invoke chat, image, TTS/STT, and video endpoints on **your** OAuth account
- Spend SuperGrok / subscription quota and trigger billable or rate-limited usage

Attack surface is **network reachability of the proxy port** plus possession of
the local client API key. Without the key, `/v1/*` returns 401. Protect the
key file and prefer loopback binds.

## Controls

| Control | Default |
|---------|---------|
| Listen address | `127.0.0.1` only (CLI) |
| Client authentication | **Required** on `/v1/*` (local API key, constant-time compare) |
| Open without key | `/health`, `/ready` only (probes) |
| Token file | `~/.xai-proxy/tokens.json` mode `0600`, dir `0700` |
| Client key file | `~/.xai-proxy/client_key` mode `0600` — salted SHA-256 only (not the secret); each `generate` overwrites |
| Upstream transport | HTTPS only |
| Host pin | discovery / token / inference must be `*.x.ai` |
| Logging | never logs access or refresh tokens |
| Path allowlist | only known xAI-native `/v1` families |
| Body size | default max **100 MiB** (media); reduces exposure if you lower it |

## Non-loopback

Binding `0.0.0.0` (or any non-loopback address) requires:

```bash
xai-proxy serve --host 0.0.0.0 --i-understand-non-loopback-bind
```

Do this only behind a trusted network, VPN, or reverse proxy **with its own auth**.
`--i-understand-non-loopback-bind` only acknowledges **non-loopback listen**;
`/v1/*` still always requires the local client API key.

## OAuth credentials

- The xAI OAuth (device-code / refresh) flow was implemented with reference to
  [Hermes Agent](https://github.com/NousResearch/hermes-agent); this project is
  independent (see [DESIGN.md](DESIGN.md#oauth-contract)).
- Uses a **public** OAuth device-code client (no client secret in the app).
- Refresh tokens are **single-use** (rotated). Concurrent processes use a file lock.
- Terminal refresh failures (`invalid_grant`) quarantine local tokens and require `login` again.
- HTTP **403** on refresh is treated as **tier/entitlement denial**, not expiry.
- Upstream policy (allowed clients, scopes, tiers) can change without notice.

## Header forwarding

Client headers other than hop-by-hop fields and `Authorization` are forwarded
to `api.x.ai`. Prefer simple API clients over a full browser pointed at the
proxy, to avoid sending unnecessary cookies.

## Outbound proxy

Operators may route **all egress** (OAuth + API) through HTTP or SOCKS5 via
`--proxy` or env vars: `XAI_PROXY_OUTBOUND`, then `ALL_PROXY` / `HTTPS_PROXY` /
`HTTP_PROXY` (and lowercase forms), with `NO_PROXY` / `no_proxy` bypass. The
proxy URL may include credentials; they are not logged. This does not add
inbound client authentication. See the README **Environment variables** section.

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

- Calling `/v1` without a client key and receiving 401 — expected
- Abuse after the operator binds `0.0.0.0` or publishes the port beyond loopback
- Upstream xAI 403 / tier / rate limits
- Using a third-party OAuth public client subject to xAI policy changes

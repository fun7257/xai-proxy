# Security model

## Threat model

`xai-proxy` is a **single-operator, machine-local** credential-attaching
forwarder for **xAI-native `/v1` paths** (chat + images + TTS + STT + video).
It is not multi-tenant SaaS.

Anyone who can connect to the listen socket can spend the operator’s
SuperGrok / xAI subscription quota (including multimodal endpoints).

## Controls

| Control | Default |
|---------|---------|
| Listen address | `127.0.0.1` only |
| Client authentication | **None** (by design) |
| Token file | `~/.xai-proxy/tokens.json` mode `0600`, dir `0700` |
| Upstream transport | HTTPS only |
| Host pin | discovery / token / inference must be `*.x.ai` |
| Logging | never logs access or refresh tokens |

## Non-loopback

Binding `0.0.0.0` (or any non-loopback address) requires:

```bash
xai-proxy serve --host 0.0.0.0 --i-understand-no-client-auth
```

Do this only behind a trusted network, VPN, or reverse proxy with its own auth.

### Docker

The container process binds `0.0.0.0:8645` (Docker NAT requirement) **with**
`--i-understand-no-client-auth`. Compose defaults to publishing only
`127.0.0.1:8645:8645` on the host. Do not change that to `0.0.0.0:8645` on a
shared machine without an outer auth layer. Token volume `xai-proxy-data` is
secret material — restrict Docker volume access.

## OAuth credentials

- Refresh tokens are **single-use** (rotated). Concurrent processes use a file lock.
- Terminal refresh failures (`invalid_grant`) quarantine local tokens and require `login` again.
- HTTP **403** on refresh is treated as **tier/entitlement denial**, not expiry.

# Docker deployment

**Local developer tool packaging** for **xai-proxy**: multi-stage build, non-root
user, read-only rootfs, host-loopback port publish, persistent OAuth volume.

This is for always-on **workstation** use — not a multi-tenant public gateway.
See [SECURITY.md](../SECURITY.md) and [SECURITY model](SECURITY.md).

## Images

| Item | Value |
|------|--------|
| Dockerfile | multi-stage (`golang:1.26.5-bookworm` → `alpine:3.21`) |
| User | UID/GID `65532` (`xai`) |
| Data | `/data` (`XAI_PROXY_HOME`) |
| Listen (in-container) | `0.0.0.0:8645` + `--i-understand-no-client-auth` |
| Host publish (compose default) | `127.0.0.1:8645` only |
| Health | `GET /health` via `wget` |

## Quick start (Compose)

```bash
cd xai-proxy

# 1) Build
docker compose build

# 2) One-time OAuth (device code — open the printed URL in a browser)
docker compose run --rm xai-proxy login --no-browser

# 3) Run
docker compose up -d

# 4) Probe
curl -s http://127.0.0.1:8645/health
curl -s http://127.0.0.1:8645/v1/models -H 'Authorization: Bearer sk-local'
```

Stop:

```bash
docker compose down
# keep volume (tokens): default
# wipe tokens: docker volume rm xai-proxy-data
```

## Plain `docker` (no Compose)

```bash
docker build -t xai-proxy:local \
  --build-arg GO_VERSION=1.26.5 \
  --build-arg VERSION=0.1.0 \
  .

# Named volume for tokens
docker volume create xai-proxy-data

docker run --rm -it \
  -v xai-proxy-data:/data \
  xai-proxy:local login --no-browser

docker run -d --name xai-proxy --restart unless-stopped \
  -p 127.0.0.1:8645:8645 \
  -v xai-proxy-data:/data \
  -e XAI_PROXY_HOME=/data \
  --read-only \
  --tmpfs /tmp:size=64m \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  xai-proxy:local
```

## Auth model in containers

| Concern | Behavior |
|---------|----------|
| Client → proxy | No API key check (same as bare metal) |
| Proxy → xAI | OAuth tokens in `/data/tokens.json` |
| Login | Interactive device code; use `--no-browser` and open URL on host |
| Re-login | `docker compose run --rm xai-proxy login --no-browser` |
| Logout | `docker compose run --rm xai-proxy logout` |

**Do not** publish `0.0.0.0:8645` on a multi-tenant host without an outer
auth gateway — anyone who can reach the port spends your SuperGrok quota.

## Environment

| Variable | Default | Meaning |
|----------|---------|---------|
| `XAI_PROXY_HOME` | `/data` | Token + state directory |
| `XAI_BASE_URL` | `https://api.x.ai/v1` | Upstream (must remain `*.x.ai`) |
| `TZ` | `UTC` | Timezone |

## Production checklist

1. **Port bind**: keep host map on `127.0.0.1` or put behind VPN / reverse proxy with auth.  
2. **Volume**: back up `xai-proxy-data` if you need durable login; treat as secret.  
3. **Updates**: rebuild image when proxy code changes; volume preserves tokens.  
4. **Health**: `docker compose ps` should show healthy; `/ready` is 503 until tokens exist.  
5. **Logs**: json-file rotation in compose (`10m` × 3).  
6. **Resources**: compose sets `mem_limit` / `cpus` / `pids_limit` as sane defaults.  

## Multi-arch (optional)

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -t your-registry/xai-proxy:0.1.0 --push .
```

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `Not logged in` on start | Run `login --no-browser` against the same volume |
| `path_not_allowed` for video | Rebuild image from latest source (old binary) |
| Healthcheck unhealthy before login | Expected until process is up; after login `/health` still OK even if `/ready` is 503 |
| 403 from xAI | Upstream tier/entitlement — not Docker networking |

# Container deployment (Apple Container / OCI)

**Local developer tool packaging** for **xai-proxy**: multi-stage Dockerfile,
non-root user, bind `0.0.0.0` inside the container for port publish, tokens on a
host volume.

This is for always-on **workstation** use — not a multi-tenant public gateway.
See [SECURITY.md](../SECURITY.md) and [SECURITY model](SECURITY.md).

Primary tested runtime on macOS: **Apple Container** (`container` CLI). The
same Dockerfile is OCI-standard and works with other builders that accept
Dockerfiles.

## Apple Container (recommended on macOS)

### Prerequisites

```bash
container --version
container system start
container system status          # should be running
# first heavy build (optional if builder already up):
container builder start
```

Prefer native `container` subcommands over Docker-style aliases (`ps` /
`images` often need plugins and fail with “Plugin not found”).

### Build

```bash
cd xai-proxy

container build -t xai-proxy:local \
  -f Dockerfile \
  --build-arg GO_VERSION=1.26.5 \
  --build-arg VERSION=0.1.0 \
  .
```

### One-time OAuth (device code)

Store tokens on the host (never commit them):

```bash
mkdir -p "$HOME/.xai-proxy-container"
chmod 700 "$HOME/.xai-proxy-container"

container run --rm -it --name xai-proxy-login \
  --volume "$HOME/.xai-proxy-container:/data" \
  --env XAI_PROXY_HOME=/data \
  xai-proxy:local login --no-browser
```

Open the printed verification URL in a browser on the host.

### Run the proxy

Publish only on the host loopback when your tooling supports it. With Apple
Container, publish a host port and use the container IP if needed
(`container list` → IP in `192.168.64.0/24`).

```bash
# stop previous instance if any
container stop xai-proxy 2>/dev/null || true
container delete xai-proxy 2>/dev/null || true

container run -d --name xai-proxy \
  --publish 8645:8645 \
  --volume "$HOME/.xai-proxy-container:/data" \
  --env XAI_PROXY_HOME=/data \
  xai-proxy:local
```

Default image `CMD` is:

```text
serve --host 0.0.0.0 --port 8645 --i-understand-no-client-auth
```

Probe:

```bash
curl -s http://127.0.0.1:8645/health
# if publish does not hit localhost, use the container IP from:
container list
curl -s http://192.168.64.x:8645/health

curl -s http://127.0.0.1:8645/v1/models -H 'Authorization: Bearer sk-local'
```

Logs / stop:

```bash
container logs xai-proxy
container stop xai-proxy
container delete xai-proxy
```

### Smoke checks (no OAuth)

```bash
container run --rm xai-proxy:local version
container run --rm xai-proxy:local help
# without tokens → process refuses to serve
container run --rm xai-proxy:local \
  serve --host 0.0.0.0 --port 8645 --i-understand-no-client-auth
# expect: Not logged in
```

## Image layout

| Item | Value |
|------|--------|
| Dockerfile | multi-stage (`golang:1.26.5-bookworm` → `alpine:3.21`) |
| User | UID/GID `65532` (`xai`) |
| Data | `/data` (`XAI_PROXY_HOME`) |
| Listen (in-container) | `0.0.0.0:8645` + `--i-understand-no-client-auth` |
| Health | `GET /health` (wget in image) |

## Auth model

| Concern | Behavior |
|---------|----------|
| Client → proxy | No API key check (local DX by design) |
| Proxy → xAI | OAuth tokens in `$XAI_PROXY_HOME/tokens.json` |
| Login | Device code; use `--no-browser` in containers |
| Re-login | Same volume + `login --no-browser` again |

**Do not** expose the published port on untrusted networks without an outer
auth layer — anyone who can connect spends your SuperGrok quota.

## Environment

| Variable | Default | Meaning |
|----------|---------|---------|
| `XAI_PROXY_HOME` | `/data` | Token + state directory |
| `XAI_BASE_URL` | `https://api.x.ai/v1` | Upstream (must remain `*.x.ai`) |
| `TZ` | `UTC` | Timezone |

## Optional: Docker Engine

If you use Docker Engine instead of Apple Container, the same Dockerfile
applies (`docker build` / `docker run`). Compose is **not required**.

```bash
docker build -t xai-proxy:local --build-arg VERSION=0.1.0 .
docker run --rm -it -v "$HOME/.xai-proxy-container:/data" -e XAI_PROXY_HOME=/data \
  xai-proxy:local login --no-browser
docker run -d --name xai-proxy -p 127.0.0.1:8645:8645 \
  -v "$HOME/.xai-proxy-container:/data" -e XAI_PROXY_HOME=/data \
  xai-proxy:local
```

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `builder is not running` | `container builder start` then rebuild |
| `Not logged in` | Run `login --no-browser` against the same volume |
| `path_not_allowed` for video | Rebuild image from latest source |
| curl to localhost fails | Use `container list` IP (`192.168.64.x:8645`) |
| 403 from xAI | Upstream tier — not container networking |

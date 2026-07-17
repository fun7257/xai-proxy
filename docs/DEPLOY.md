# Container image deployment

How to build and run the **xai-proxy** OCI image on your machine.  
Chinese: [DEPLOY_zh.md](DEPLOY_zh.md).  
Security model: [SECURITY.md](SECURITY.md). Architecture: [DESIGN.md](DESIGN.md).

> **Local single-operator use only.** Prefer publishing the port to **127.0.0.1**.  
> Not a multi-tenant or public gateway.

## What the image does

| Item | Value |
|------|--------|
| Default process | `serve --host 0.0.0.0 --port 7257 --i-understand-non-loopback-bind` |
| Config / secrets dir | `XAI_PROXY_HOME` (default **`/data`**) |
| Entrypoint (`serve` only) | If no client key → `generate` (prints key once to logs); if OAuth not ready → `login --no-browser`; then serve |
| Other commands | `generate` / `login` / `status` / `logout` / `version` pass through (no bootstrap) |
| Probes | `GET /health`, `GET /ready` (no client key) |
| API | `http://127.0.0.1:7257/v1/*` requires `Authorization: Bearer <client_key>` |

Image user: non-root `xai` (uid/gid **65532**). Files under `/data` must be writable by that user (or by the uid you pass with `-u`).

## Prerequisites

- **Apple Container** (`container` CLI on macOS / Apple silicon), **or** Docker / compatible OCI runtime
- SuperGrok or X Premium+ (upstream OAuth / API access)
- Browser available for first-time device-code login

## Build

From the repository root:

```bash
# Apple Container
container build -t xai-proxy:local -f Dockerfile .

# Docker (or compatible)
docker build -t xai-proxy:local .
```

## Run (recommended: bind-mount host config)

Share the host config directory so the container reuses the same tokens / key verifier as a native install, and file ownership matches your user.

```bash
mkdir -p "$HOME/.xai-proxy"

# Apple Container
container run -d --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v "$HOME/.xai-proxy:/data" \
  -u "$(id -u):$(id -g)" \
  xai-proxy:local

# Docker
docker run -d --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v "$HOME/.xai-proxy:/data" \
  -u "$(id -u):$(id -g)" \
  xai-proxy:local
```

| Flag | Purpose |
|------|---------|
| `-p 127.0.0.1:7257:7257` | Publish only on host loopback |
| `-v "$HOME/.xai-proxy:/data"` | Persist `tokens.json` + `client_key` on the host |
| `-u "$(id -u):$(id -g)"` | Match host directory ownership (avoids permission errors without root) |

Foreground / interactive (handy for first device login):

```bash
container run --rm --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v "$HOME/.xai-proxy:/data" \
  -u "$(id -u):$(id -g)" \
  -it \
  xai-proxy:local
```

### Alternative: named volume

```bash
container volume create xai-data   # or: docker volume create xai-data

container run -d --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v xai-data:/data \
  -u root \
  xai-proxy:local
```

Empty named volumes are often **root-owned**. The image user `xai` (65532) cannot write `/data` until ownership is fixed, so either:

- run once with `-u root` (works, less ideal), or  
- use the host bind-mount recipe above (preferred).

## First boot

1. Start the container (see above).
2. Read logs for bootstrap:

   ```bash
   container logs -f xai-proxy
   # docker logs -f xai-proxy
   ```

3. **Client API key** — if `/data/client_key` was missing, entrypoint runs `generate` and prints:

   ```text
   xai-proxy-init: CLIENT API KEY (shown once — save it from container logs now):
     sk-xai-...
   ```

   Save this immediately. The disk stores only a salted hash; there is no `key show`. Losing the plaintext means run `generate` again (old clients break).

4. **OAuth** — if not already authorized, entrypoint runs `login --no-browser`. Open the device URL, enter the code, wait until logs show serve started.

5. Check readiness:

   ```bash
   curl -sS http://127.0.0.1:7257/health
   curl -sS http://127.0.0.1:7257/ready
   ```

   Expect `ready` / `state: ready` after login succeeds.

## Client configuration

| Setting | Value |
|---------|--------|
| Base URL | `http://127.0.0.1:7257/v1` |
| API key | Local client key from first-boot logs (`sk-xai-…`) |

Example chat request:

```bash
export KEY='sk-xai-...'   # from container logs; never commit

curl -sS http://127.0.0.1:7257/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "grok-4.5",
    "messages": [{"role": "user", "content": "hi"}]
  }'
```

Streaming: add `"stream": true` and use `curl -N`.

Path policy (native xAI routes, OpenAI full-compat only where shapes match): see [DESIGN.md](DESIGN.md) and root README.

## Day-2 operations

```bash
# Status (OAuth + whether client key verifier exists)
container exec xai-proxy xai-proxy status
# or one-shot:
container run --rm -v "$HOME/.xai-proxy:/data" -u "$(id -u):$(id -g)" xai-proxy:local status

# Logs / stop / remove
container logs xai-proxy
container stop xai-proxy
container rm xai-proxy

# Rotate local client key (invalidates previous key)
container run --rm -v "$HOME/.xai-proxy:/data" -u "$(id -u):$(id -g)" xai-proxy:local generate

# Force re-login (device flow)
container run --rm -it -v "$HOME/.xai-proxy:/data" -u "$(id -u):$(id -g)" xai-proxy:local login --no-browser

# Delete named volume (if you used one)
container volume rm xai-data
```

With a host bind-mount, deleting data is just removing files under `~/.xai-proxy` (tokens / client_key). Treat that directory as secret.

## Outbound proxy (optional)

Egress for OAuth + upstream API (not inbound clients):

```bash
container run -d --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v "$HOME/.xai-proxy:/data" \
  -u "$(id -u):$(id -g)" \
  -e XAI_PROXY_OUTBOUND=socks5://host.docker.internal:1080 \
  xai-proxy:local
```

Priority: CLI `--proxy` → `XAI_PROXY_OUTBOUND` → `ALL_PROXY` → `HTTPS_PROXY` → `HTTP_PROXY`.  
See root README for schemes and `NO_PROXY`.

To pass flags to `serve` via Apple Container / Docker, append after the image name:

```bash
container run ... xai-proxy:local \
  serve --host 0.0.0.0 --port 7257 --i-understand-non-loopback-bind --proxy socks5://...
```

(Entrypoint still runs key + OAuth bootstrap when the first argument is `serve`.)

## Environment variables

| Variable | Role inside container |
|----------|------------------------|
| `XAI_PROXY_HOME` | Config dir (image default `/data`; keep in sync with your volume mount) |
| `XAI_PROXY_OUTBOUND` | Egress proxy URL |
| `ALL_PROXY` / `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` | Standard egress proxy env |
| `XAI_BASE_URL` | Optional inference base at login (HTTPS `*.x.ai` only) |
| `TZ` | Timezone (image default `UTC`) |

There is **no** env injection for the local client API key (only `generate`).

## Permissions checklist

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `permission denied` writing `client_key` / `tokens.json` | Volume root-owned; process is uid 65532 | Bind-mount host dir + `-u "$(id -u):$(id -g)"`, or chown the mount, or temporary `-u root` |
| `/v1` → unauthorized / missing key | No client key in requests | Use key from first-boot logs; re-`generate` if lost |
| `/ready` not ready | OAuth not finished or reauth required | Complete device login; check `status` |
| Port not reachable on host | Publish not bound / wrong IP | Use `-p 127.0.0.1:7257:7257`; confirm container is running |

## Security reminders

- Publish to **loopback** on the host (`127.0.0.1`), not `0.0.0.0` on the host publish side.
- Treat container logs (first-boot key) and `~/.xai-proxy` / volume contents as secrets.
- Do not push images that bake in tokens or keys; data belongs on the volume only.
- Full threat model: [SECURITY.md](SECURITY.md).

## Image layout (reference)

```text
Dockerfile              multi-stage build (Go → Alpine runtime)
docker-entrypoint.sh    serve bootstrap: generate → login → exec
/usr/local/bin/xai-proxy
/data                   XAI_PROXY_HOME (mount here)
```

Default `CMD`:

```text
serve --host 0.0.0.0 --port 7257 --i-understand-non-loopback-bind
```

`0.0.0.0` inside the container is required so port publish works; **host** publish should still be loopback-only.

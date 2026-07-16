# Docker Engine（可选）

标准 **Docker Engine** 使用同一份 `Dockerfile`。  
**macOS 推荐优先阅读 [CONTAINER.md](CONTAINER.md)**（Apple Container，已实测）。

## Build

```bash
cd xai-proxy
docker build -t xai-proxy:local \
  --build-arg GO_VERSION=1.26.5 \
  --build-arg VERSION=0.1.0 \
  .
```

## First run (login if needed, then serve)

Default image command is `start` (`--no-browser` device login → write tokens → serve).

```bash
mkdir -p "$HOME/.xai-proxy-container" && chmod 700 "$HOME/.xai-proxy-container"

docker run -d --name xai-proxy \
  -p 127.0.0.1:8645:8645 \
  -v "$HOME/.xai-proxy-container:/data" \
  -e XAI_PROXY_HOME=/data \
  xai-proxy:local

docker logs -f xai-proxy   # open printed accounts.x.ai URL, then proxy starts
curl -s http://127.0.0.1:8645/health
```

宿主机建议只绑定 **127.0.0.1**。代理无客户端鉴权；勿对公网裸奔。

完整环境变量、安全说明见 [CONTAINER.md](CONTAINER.md) 与 [SECURITY.md](../SECURITY.md)。

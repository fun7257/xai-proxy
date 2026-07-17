# 镜像部署

如何在本机构建并运行 **xai-proxy** OCI 镜像。  
英文: [DEPLOY.md](DEPLOY.md)。  
安全模型: [SECURITY_zh.md](SECURITY_zh.md)。架构: [DESIGN_zh.md](DESIGN_zh.md)。

> **仅限本机单人使用。** 端口请优先发布到 **127.0.0.1**。  
> 不是多租户服务，也不是公网网关。

## 镜像行为

| 项 | 说明 |
|----|------|
| 默认进程 | `serve --host 0.0.0.0 --port 7257 --i-understand-non-loopback-bind` |
| 配置 / 密钥目录 | `XAI_PROXY_HOME`（默认 **`/data`**） |
| 入口（仅 `serve`） | 无 client key → `generate`（明文 key 只打一次日志）；OAuth 未就绪 → `login --no-browser`；然后 serve |
| 其它命令 | `generate` / `login` / `status` / `logout` / `version` 直接透传（不做 bootstrap） |
| 探活 | `GET /health`、`GET /ready`（无需 client key） |
| 业务 API | `http://127.0.0.1:7257/v1/*` 必须带 `Authorization: Bearer <client_key>` |

镜像默认用户：非 root 的 `xai`（uid/gid **65532**）。`/data` 必须对该用户（或你用 `-u` 指定的 uid）可写。

## 前置条件

- **Apple Container**（macOS / Apple silicon 上的 `container` CLI），或 Docker / 兼容 OCI 运行时
- SuperGrok 或 X Premium+（上游 OAuth / API 权限）
- 首次 device-code 登录需要浏览器

## 构建

在仓库根目录：

```bash
# Apple Container
container build -t xai-proxy:local -f Dockerfile .

# Docker（或兼容实现）
docker build -t xai-proxy:local .
```

## 正式发版产物（GitHub Actions）

在 GitHub 上 **Publish Release**（tag `v*`）会触发 [`.github/workflows/release.yml`](../.github/workflows/release.yml)。  
也可手动：**Actions → Release → Run workflow**（填写 tag，如 `v0.1.0`；可开关是否推镜像 / 上传二进制）。
| 产物 | 平台 / 架构 |
|------|-------------|
| **OCI 镜像** → `ghcr.io/<owner>/xai-proxy` | `linux/amd64`、`linux/arm64` |
| **二进制**（Release Assets） | linux / darwin / windows × amd64 + arm64 |
| **校验和** | 同 Release 上的 `SHA256SUMS` |

镜像 tag 示例（`v0.1.1`）：`0.1.1`、`0.1`、`v0.1.1`、`latest`。

```bash
# 拉取多架构镜像（Docker / 兼容实现）
docker pull ghcr.io/<owner>/xai-proxy:latest
# 或固定版本: ghcr.io/<owner>/xai-proxy:0.1.1

# 二进制：从 GitHub Release 下载，例如
#   xai-proxy_0.1.1_darwin_arm64.tar.gz
#   xai-proxy_0.1.1_linux_amd64.tar.gz
#   xai-proxy_0.1.1_windows_amd64.zip
```

将 `<owner>` 换成仓库 owner（本仓库为 `fun7257`）。  
若 package 为 private，需 `docker login ghcr.io`（PAT 带 `read:packages`）。
## 运行（推荐：绑定宿主机配置目录）

把宿主机配置目录挂进容器，可与本机原生安装共用 token / key 校验串，并用本机 uid 避免权限问题。

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

| 参数 | 作用 |
|------|------|
| `-p 127.0.0.1:7257:7257` | 仅发布到宿主机 loopback |
| `-v "$HOME/.xai-proxy:/data"` | 在宿主机持久化 `tokens.json` 与 `client_key` |
| `-u "$(id -u):$(id -g)"` | 与目录属主一致，通常不必 root |

前台交互（方便第一次 device 登录）：

```bash
container run --rm --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v "$HOME/.xai-proxy:/data" \
  -u "$(id -u):$(id -g)" \
  -it \
  xai-proxy:local
```

### 备选：命名卷

```bash
container volume create xai-data   # 或: docker volume create xai-data

container run -d --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v xai-data:/data \
  -u root \
  xai-proxy:local
```

新建命名卷的空目录往往是 **root 属主**。镜像用户 `xai`（65532）写不了 `/data`，除非：

- 临时用 `-u root`（能跑，但不优雅），或  
- 改用上文的宿主机 bind-mount（推荐）。

## 首次启动

1. 按上面命令启动容器。
2. 看日志里的 bootstrap：

   ```bash
   container logs -f xai-proxy
   # docker logs -f xai-proxy
   ```

3. **Client API key** — 若原先没有 `/data/client_key`，入口会 `generate` 并打印：

   ```text
   xai-proxy-init: CLIENT API KEY (shown once — save it from container logs now):
     sk-xai-...
   ```

   **立刻保存。** 磁盘只存加盐哈希，没有 `key show`。丢了明文只能再 `generate`（旧 key 立即失效）。

4. **OAuth** — 若尚未授权，入口会执行 `login --no-browser`。打开 device URL、输入 code，等到日志出现 serve 启动即可。

5. 检查就绪：

   ```bash
   curl -sS http://127.0.0.1:7257/health
   curl -sS http://127.0.0.1:7257/ready
   ```

   登录成功后应看到 `ready` / `state: ready`。

## 客户端配置

| 项 | 值 |
|----|-----|
| Base URL | `http://127.0.0.1:7257/v1` |
| API Key | 首次日志中的本地 client key（`sk-xai-…`） |

对话示例：

```bash
export KEY='sk-xai-...'   # 来自容器日志；勿提交到仓库

curl -sS http://127.0.0.1:7257/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "grok-4.5",
    "messages": [{"role": "user", "content": "hi"}]
  }'
```

流式：加 `"stream": true`，并用 `curl -N`。

路径策略（xAI 原生路径；仅路径+body 与 OpenAI 一致时额外全兼容）：见 [DESIGN_zh.md](DESIGN_zh.md) 与根目录 README。

## 日常运维

```bash
# 状态（OAuth + client key 是否已配置）
container exec xai-proxy xai-proxy status
# 或一次性：
container run --rm -v "$HOME/.xai-proxy:/data" -u "$(id -u):$(id -g)" xai-proxy:local status

# 日志 / 停止 / 删除容器
container logs xai-proxy
container stop xai-proxy
container rm xai-proxy

# 轮换本地 client key（旧 key 失效）
container run --rm -v "$HOME/.xai-proxy:/data" -u "$(id -u):$(id -g)" xai-proxy:local generate

# 强制重新登录（device 流程）
container run --rm -it -v "$HOME/.xai-proxy:/data" -u "$(id -u):$(id -g)" xai-proxy:local login --no-browser

# 删除命名卷（若用了命名卷）
container volume rm xai-data
```

若使用 bind-mount，清理数据即删除 `~/.xai-proxy` 下相关文件（tokens / client_key）。该目录按密钥保管。

## 出站代理（可选）

仅影响 OAuth 与上游 API 出站（不影响入站客户端）：

```bash
container run -d --name xai-proxy \
  -p 127.0.0.1:7257:7257 \
  -v "$HOME/.xai-proxy:/data" \
  -u "$(id -u):$(id -g)" \
  -e XAI_PROXY_OUTBOUND=socks5://host.docker.internal:1080 \
  xai-proxy:local
```

优先级：CLI `--proxy` → `XAI_PROXY_OUTBOUND` → `ALL_PROXY` → `HTTPS_PROXY` → `HTTP_PROXY`。  
协议与 `NO_PROXY` 见根目录 README。

向 `serve` 追加参数时，写在镜像名之后：

```bash
container run ... xai-proxy:local \
  serve --host 0.0.0.0 --port 7257 --i-understand-non-loopback-bind --proxy socks5://...
```

（当第一个参数是 `serve` 时，入口仍会做 key + OAuth bootstrap。）

## 环境变量

| 变量 | 容器内作用 |
|------|------------|
| `XAI_PROXY_HOME` | 配置目录（镜像默认 `/data`，需与挂载点一致） |
| `XAI_PROXY_OUTBOUND` | 出站代理 URL |
| `ALL_PROXY` / `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` | 标准出站代理环境变量 |
| `XAI_BASE_URL` | 登录时可选的推理 base（仅 HTTPS `*.x.ai`） |
| `TZ` | 时区（镜像默认 `UTC`） |

**没有**通过环境变量注入本地 client API key 的方式（只能 `generate`）。

## 权限与排障

| 现象 | 常见原因 | 处理 |
|------|----------|------|
| 写 `client_key` / `tokens.json` 报 `permission denied` | 卷为 root 属主，进程是 65532 | 宿主机目录 + `-u "$(id -u):$(id -g)"`，或 chown 挂载点，或临时 `-u root` |
| `/v1` 未授权 / 缺 key | 请求未带 client key | 使用首次日志中的 key；丢失则重新 `generate` |
| `/ready` 未就绪 | OAuth 未完成或需 reauth | 完成 device 登录；检查 `status` |
| 宿主机访问不了端口 | 未正确 publish | 使用 `-p 127.0.0.1:7257:7257`；确认容器在跑 |

## 安全提醒

- 宿主机端口发布用 **loopback**（`127.0.0.1`），不要把 host 侧绑到公网。
- 首次日志中的 key、以及 `~/.xai-proxy` / volume 内容均按密钥处理。
- 不要把 token / key 打进镜像层；状态只放在卷或挂载目录。
- 完整威胁模型见 [SECURITY_zh.md](SECURITY_zh.md)。

## 镜像结构（参考）

```text
Dockerfile              多阶段构建（Go → Alpine 运行时）
docker-entrypoint.sh    serve 引导：generate → login → exec
/usr/local/bin/xai-proxy
/data                   XAI_PROXY_HOME（数据挂这里）
```

默认 `CMD`：

```text
serve --host 0.0.0.0 --port 7257 --i-understand-non-loopback-bind
```

容器内 `0.0.0.0` 是为了端口发布能生效；**宿主机** publish 仍应只绑 loopback。

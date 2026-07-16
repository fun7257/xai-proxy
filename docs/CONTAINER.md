# Apple Container 部署指南

面向 macOS 上的 **Apple Container**（`container` CLI）。  
本项目定位为**本机开发者工具**，不是多租户公网网关。安全边界见 [SECURITY.md](../SECURITY.md)。

Dockerfile 是标准 OCI 多阶段构建；**不需要** docker compose。

| 项 | 值 |
|----|-----|
| 镜像名示例 | `xai-proxy:local` |
| 数据目录（宿主机） | `~/.xai-proxy-container` |
| 容器内数据 | `/data`（`XAI_PROXY_HOME`） |
| 容器内监听 | `0.0.0.0:8645` |
| 默认用户 | `xai`（UID 65532） |

---

## 1. 前置条件

```bash
container --version
# 期望: container CLI version 1.0.0 ...

container system start
container system status
# status 应为 running

# 首次构建或 builder 未启动时：
container builder start
container builder status
```

**注意：**

- 优先用内置子命令：`run`、`list`、`image list`、`build`、`logs`、`stop`、`delete`。
- 少用 Docker 风格别名（`ps`、`images` 等常报 *Plugin not found*）。
- 默认网络段通常为 `192.168.64.0/24`；端口发布后若 `127.0.0.1` 不通，用 `container list` 看容器 IP。

---

## 2. 构建镜像

```bash
cd xai-proxy

container build -t xai-proxy:local \
  -f Dockerfile \
  --build-arg GO_VERSION=1.26.5 \
  --build-arg VERSION=0.1.0 \
  .
```

或：

```bash
make container-build
```

成功后可用：

```bash
container image list | grep xai-proxy
container run --rm xai-proxy:local version
# 0.1.0
```

---

## 3. 一次性 OAuth 登录

设备码流程：容器打印 URL，在宿主机浏览器打开完成授权。Token 写在**宿主机目录**，勿提交 git。

```bash
mkdir -p "$HOME/.xai-proxy-container"
chmod 700 "$HOME/.xai-proxy-container"

container run --rm -it --name xai-proxy-login \
  --volume "$HOME/.xai-proxy-container:/data" \
  --env XAI_PROXY_HOME=/data \
  xai-proxy:local login --no-browser
```

或：`make container-login`（默认 `DATA_DIR=$HOME/.xai-proxy-container`）。

重新登录：对**同一 volume** 再跑一次上述命令。  
登出：

```bash
container run --rm \
  --volume "$HOME/.xai-proxy-container:/data" \
  --env XAI_PROXY_HOME=/data \
  xai-proxy:local logout
```

---

## 4. 常驻运行

```bash
# 清理旧实例（可忽略报错）
container stop xai-proxy 2>/dev/null || true
container delete xai-proxy 2>/dev/null || true

container run -d --name xai-proxy \
  --publish 8645:8645 \
  --volume "$HOME/.xai-proxy-container:/data" \
  --env XAI_PROXY_HOME=/data \
  xai-proxy:local
```

或：`make container-run`。

镜像默认 `CMD`：

```text
serve --host 0.0.0.0 --port 8645 --i-understand-no-client-auth
```

容器内必须绑 `0.0.0.0` 才能做端口发布。代理**不对客户端鉴权**——勿把端口暴露到不可信网络。

### 探活

```bash
curl -s http://127.0.0.1:8645/health
curl -s http://127.0.0.1:8645/ready
curl -s http://127.0.0.1:8645/v1/models \
  -H 'Authorization: Bearer sk-local'
```

若本机 `127.0.0.1:8645` 不通：

```bash
container list
# 找到 xai-proxy 的 IP，例如 192.168.64.7
curl -s http://192.168.64.7:8645/health
```

客户端 Base URL：`http://127.0.0.1:8645/v1`（或上述容器 IP）。

### 日志与停止

```bash
container logs xai-proxy
container stop xai-proxy
container delete xai-proxy
# 或: make container-stop
```

---

## 5. 无 token 时的冒烟检查

```bash
container run --rm xai-proxy:local version
container run --rm xai-proxy:local help

# 未登录应拒绝 serve
container run --rm xai-proxy:local \
  serve --host 0.0.0.0 --port 8645 --i-understand-no-client-auth
# 期望输出含: Not logged in
```

---

## 6. 环境变量

| 变量 | 默认 | 含义 |
|------|------|------|
| `XAI_PROXY_HOME` | `/data` | Token 与状态目录 |
| `XAI_BASE_URL` | `https://api.x.ai/v1` | 上游（必须仍是 `*.x.ai`） |
| `TZ` | `UTC` | 时区 |

Token 文件：`$XAI_PROXY_HOME/tokens.json`（应用内 `0600`）。

---

## 7. 镜像结构（Dockerfile）

| 阶段 | 基础镜像 | 作用 |
|------|----------|------|
| builder | `golang:1.26.5-bookworm` | 静态编译 `xai-proxy` |
| runtime | `alpine:3.21` | ca-certificates、wget（HEALTHCHECK）、非 root |

运行用户 `xai:xai`（65532），工作目录 `/data`。

---

## 8. 故障排查

| 现象 | 处理 |
|------|------|
| `builder is not running` | `container builder start` 后重建 |
| `Not logged in` | 对同一 volume 再 `login --no-browser` |
| `path_not_allowed`（如 video） | 用最新源码重建镜像 |
| curl 本机不通 | `container list` 用 `192.168.64.x:8645` |
| 上游 HTTP 403 | 订阅/档位问题，不是容器网络 |
| Docker 别名 Plugin not found | 改用 `container list` / `container image list` 等 |

---

## 9. Makefile 快捷命令

```bash
make container-build    # 构建 xai-proxy:local
make container-login    # 交互设备码登录
make container-run      # 后台运行 --publish 8645:8645
make container-stop     # stop + delete
```

数据目录可用：`make container-run DATA_DIR=/path/to/dir`。

---

## 相关文档

- [SECURITY.md](../SECURITY.md) — 开源定位与安全策略  
- [SECURITY model](SECURITY.md) — 威胁模型与控制  
- [DOCKER.md](DOCKER.md) — 可选：Docker Engine 简要命令（非 compose）  
- [example/](../example/) — 全 path 请求示例  

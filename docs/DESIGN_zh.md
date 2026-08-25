# 设计说明

架构与产品契约说明。开发风格与安全准则见根目录 [AGENTS.md](../AGENTS.md)（仅英文）；安全与威胁模型见 [SECURITY_zh.md](SECURITY_zh.md)。  
英文主文档：[DESIGN.md](DESIGN.md)。

## 项目是什么

- **本机开发者工具**（单操作者、非 SaaS、非 xAI 官方产品）
- 本机 xAI OAuth（设备码）登录 + 本地 **xAI 原生 `/v1/*` 转发代理**（聊天 + 全模态）
- 开源定位与免责：根目录 `README.md` / `README_zh.md`、`LICENSE`（MIT）；安全见 [SECURITY_zh.md](SECURITY_zh.md)

## 鉴权双层模型

| 层 | 凭证 | 方向 | 存储 |
|----|------|------|------|
| 本地客户端 | `sk-xai-…`（明文仅在 generate 时出现） | 客户端 → proxy | 磁盘：`client_key` 中加盐 SHA-256（仅最新） |
| 上游 OAuth | access / refresh | proxy → xAI | `~/.xai-proxy/tokens.json` |

- `/v1/*`：对提交的 key 做同样 hash → constant-time 与磁盘校验串比较 → 剥离客户端鉴权头 → 挂 OAuth Bearer
- `/health`、`/ready`：开放（探针）
- CLI：`xai-proxy generate`（覆盖旧校验串；明文只在 stdout **展示一次**，磁盘不存明文）
- 校验串格式：`v1$sha256$<salt_hex>$<hash_hex>`（stdlib `crypto/sha256`）

## 分层

1. **Auth**（`internal/auth`）— 设备码登录、OIDC discovery、refresh、JWT skew、host pin  
2. **Store**（`internal/store`）— `tokens.json` / `client_key` 原子读写 + flock  
3. **Credential manager**（`internal/credential`）— `GetBearer` / `ForceRefresh` / `Status`  
4. **Proxy**（`internal/proxy`）— `net/http` 透传转发 + path allowlist + client auth  
5. **Outbound**（`internal/outbound`）— HTTP/SOCKS 出站代理策略  
6. **CLI**（`internal/cli`）— `generate` / `login` / `serve` / `status` / …

## 路径策略

### 原则

1. **全部转发的都是 xAI 原生 path**（对话 / 图片 / 语音 / 视频一视同仁）
2. 其中**一部分**与 OpenAI 同 path + 同 body 形状 → **额外**可被 OpenAI SDK 当 `base_url` 用（全兼容）
3. **做不到全兼容 → 不写 shim**；客户端直接打 xAI 原生 path

### 支持的 xAI 原生 path（`/v1` 下）

| Path | 能力 | 额外 OpenAI 全兼容 |
|------|------|-------------------|
| `/chat/completions` | 对话 | 是 |
| `/responses` | Responses 对话 | 是 |
| `/completions` | Completions | 是 |
| `/embeddings` | 向量 | 是 |
| `/models` | 模型列表 | 是 |
| `/images/generations` | 文生图 | 否（xAI 扩展字段） |
| `/images/edits` | 图编辑（JSON） | 否 |
| `/tts` | 文字转语音 | 否 |
| `/stt` | 语音转文字 multipart | 否 |
| `/videos/generations` | 视频生成 | 否 |
| `/videos/edits` | 视频编辑 | 否 |
| `/videos/extensions` | 视频延长 | 否 |
| `/videos/{id}` | 视频任务状态 | 否 |

- Exact allow：上表固定 path  
- Pattern allow：`/videos/{id}` 异步状态  
- Body 上限默认 **100 MiB**

### 明确拒绝的 OpenAI-only 音频 path（不 shim）

- `/audio/speech` → 用原生 `/tts`
- `/audio/transcriptions` → 用原生 `/stt`
- `/audio/translations` → 不映射

实现入口：`internal/proxy/allowlist.go`。

## OAuth 契约

> **说明：** 本项目中的 xAI OAuth（设备码登录、token 刷新及相关鉴权流程）
> 实现参考了 **[Hermes Agent](https://github.com/NousResearch/hermes-agent)**
> 的社区集成方式。xai-proxy 为独立项目，**不**隶属于 Hermes Agent、Nous
> Research 或 xAI。

| 项 | 值 |
|----|-----|
| Client ID | `b1a00492-073a-47ea-816f-4c329264a828`（public device client） |
| Scope | `openid profile email offline_access grok-cli:access api:access` |
| Device | `POST https://auth.x.ai/oauth2/device/code` |
| Token | discovery 的 `token_endpoint`（通常 `https://auth.x.ai/oauth2/token`） |
| API | `https://api.x.ai/v1` |

Refresh：单次使用、原子写回；**403** = 档位拒绝；`invalid_grant` 等 terminal → quarantine / re-login。

## CLI

```text
xai-proxy generate                          # 生成 client key（覆盖；只展示一次）
xai-proxy [--proxy URL] login   [--no-browser] [--proxy URL]
xai-proxy [--proxy URL] serve   [--host ...] [--port ...] [--proxy URL] [--header-timeout ...]
xai-proxy status | logout | version
```

`--header-timeout`（默认 **15 分钟**，`0` 关闭）是等待上游 **响应头** 的上限。非 SSE 的 `/chat/completions` 通常要等模型思考结束后才发响应头；原先 120 秒会把这类请求掐掉。SSE 仍无总超时（分片之间空闲 3 分钟）。

### 出站代理（OAuth + API 共用）

| 来源 | 说明 |
|------|------|
| CLI | `--proxy socks5://…` / `http://…`（优先） |
| Env | 见下方 **环境变量** |
| Bypass | `NO_PROXY` / `no_proxy` |

实现：`internal/outbound`。

### 配置目录

| 路径 | 用途 |
|------|------|
| `~/.xai-proxy/tokens.json` | OAuth tokens（`0600`） |
| `~/.xai-proxy/client_key` | 仅加盐 SHA-256 校验串（`0600`）；每次 `generate` 覆盖 |

默认目录：`~/.xai-proxy`（`XAI_PROXY_HOME`）。

### 环境变量

| 变量 | 作用 |
|------|------|
| `XAI_PROXY_HOME` | 配置目录（默认 `~/.xai-proxy`） |
| `XAI_PROXY_OUTBOUND` | OAuth + API 出站代理（环境变量中最高优先） |
| `ALL_PROXY` / `all_proxy` | 标准统一代理 |
| `HTTPS_PROXY` / `https_proxy` | 标准 HTTPS 代理 |
| `HTTP_PROXY` / `http_proxy` | 标准 HTTP 代理 |
| `NO_PROXY` / `no_proxy` | 代理绕过列表 |
| `XAI_BASE_URL` | 登录时可选上游 API base（默认 `https://api.x.ai/v1`；仅 HTTPS `*.x.ai`） |

出站代理优先级：`--proxy` → `XAI_PROXY_OUTBOUND` → `ALL_PROXY` → `HTTPS_PROXY` → `HTTP_PROXY` → 直连。  
本地 client API key **无**环境变量注入（仅 `generate`）。

## 技术栈

- Go **1.26.5**
- HTTP：**仅** `net/http`（无第三方路由框架）
- 默认零第三方；出站 SOCKS：`golang.org/x/net`

## 明确不做（产品范围）

- 多租户 SaaS / 公网身份体系（仅有本地共享 client key，不是完整用户系统）
- 冒充 xAI 官方产品
- Twitter 官方 OAuth API
- OpenAI→xAI TTS/STT/video 假兼容 shim
- WebSocket Voice realtime 网关（当前非目标）

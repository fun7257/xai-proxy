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
| 本地客户端 | `client_key`（`sk-xai-…`） | 客户端 → proxy | `~/.xai-proxy/client_key` 或 `XAI_PROXY_CLIENT_KEY` |
| 上游 OAuth | access / refresh | proxy → xAI | `~/.xai-proxy/tokens.json` |

- `/v1/*`：校验 client key → 剥离客户端鉴权头 → 挂 OAuth Bearer 转发
- `/health`、`/ready`：开放（探针）
- CLI：`xai-proxy key show` / `key regenerate`

## 分层

1. **Auth**（`internal/auth`）— 设备码登录、OIDC discovery、refresh、JWT skew、host pin  
2. **Store**（`internal/store`）— `tokens.json` / `client_key` 原子读写 + flock  
3. **Credential manager**（`internal/credential`）— `GetBearer` / `ForceRefresh` / `Status`  
4. **Proxy**（`internal/proxy`）— `net/http` 透传转发 + path allowlist + client auth  
5. **Outbound**（`internal/outbound`）— HTTP/SOCKS 出站代理策略  
6. **CLI**（`internal/cli`）— `start` / `login` / `serve` / `key` / …

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
xai-proxy [--proxy URL] start   [options]   # 无 token 则 login，再 serve
xai-proxy [--proxy URL] login   [--no-browser] [--proxy URL]
xai-proxy [--proxy URL] serve   [--host ...] [--port ...] [--proxy URL]
xai-proxy key show | key regenerate
xai-proxy status | logout | version
```

### 出站代理（OAuth + API 共用）

| 来源 | 说明 |
|------|------|
| CLI | `--proxy socks5://…` / `http://…`（优先） |
| Env | `XAI_PROXY_OUTBOUND`，然后 `ALL_PROXY` / `HTTPS_PROXY` / `HTTP_PROXY` |
| Bypass | `NO_PROXY` / `no_proxy` |

实现：`internal/outbound`。

### 配置目录

| 路径 / 变量 | 用途 |
|-------------|------|
| `~/.xai-proxy/tokens.json` | OAuth tokens（`0600`） |
| `~/.xai-proxy/client_key` | 本地客户端密钥（`0600`） |
| `XAI_PROXY_HOME` | 覆盖配置目录 |
| `XAI_PROXY_CLIENT_KEY` | 覆盖客户端密钥（可选） |

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

# xai-proxy — Development Guide

给在本仓库 `xai-proxy/` 下工作的人类与 AI 助手的硬约束。**优先于口头习惯。**

## 项目是什么

- **本机开发者工具**（单操作者、非 SaaS、非 xAI 官方产品）
- 本机 xAI OAuth（设备码）登录 + 本地 **xAI 原生 `/v1/*` 转发代理**（聊天 + 全模态）
- 操作者 `login` 一次后，客户端免鉴权使用（默认仅 `127.0.0.1`）
- 协议对齐 Hermes 一类 `xai-oauth` 设备码流程，但**独立实现**，不依赖 hermes-agent 运行时
- 开源定位与免责：见根目录 `README.md`、`SECURITY.md`、`LICENSE`（MIT）

## 技术硬约束

1. Go **1.26.5**（`go.mod` 必须写 `go 1.26.5`）
2. HTTP **仅** 标准库 `net/http`（Server / Client / ServeMux）
   - 禁止：chi、echo、gin、fiber、gorilla/mux、fasthttp 等
3. **非必要零第三方依赖**
   - 能用 stdlib 解决的禁止加依赖
   - 确需引入：优先 `golang.org/x/*`，其次高星、持续维护的事实标准库
   - 新增 `require` 须说明：为何 stdlib 不够 + 维护现状
4. 依赖：默认零第三方；出站 SOCKS 允许 `golang.org/x/net`（官方 x 包）

## 产品核心要点

1. 不对客户端做鉴权：忽略/剥离客户端 `Authorization`，挂载 OAuth Bearer
2. 安全边界 = 默认 bind `127.0.0.1` + tokens 文件权限；非回环须显式确认
3. Token：`~/.xai-proxy/tokens.json`（0600），与 Hermes `auth.json` 解耦
4. Refresh token 单次使用：刷新后必须原子写回；文件锁 + singleflight
5. Host 钉死：token / discovery / inference 仅 `https` + `*.x.ai`
6. 403 refresh = 档位拒绝，不是过期；`invalid_grant` 才 quarantine / 要求 re-login
7. **透传 body，不改协议形状**；路径按 xAI 原生 allow 策略（见下）
8. SSE / 二进制 / multipart 流式不得错误改写；body 上限默认 **100 MiB**（媒体）
9. 日志永不输出 `access_token` / `refresh_token`

## 路径策略（强制）

### 原则

1. **全部转发的都是 xAI 原生 path**（对话 / 图片 / 语音 / 视频一视同仁）
2. 其中**一部分**与 OpenAI 同 path + 同 body 形状 → **额外**可被 OpenAI SDK 当 base_url 用（全兼容）
3. **做不到全兼容 → 不写 shim**；客户端直接打 xAI 原生 path，不发明 `/audio/*` 等假映射

### 当前支持的 xAI 原生 path（`/v1` 下）

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

### 明确拒绝的 OpenAI-only 音频 path（不 shim）

- `/audio/speech` → 用原生 `/tts`
- `/audio/transcriptions` → 用原生 `/stt`
- `/audio/translations` → 不映射

新增 path：改 `internal/proxy/allowlist.go` 的 `ExactAllowedPaths` 或 `PathAllowed`。

## OAuth 契约（与 Hermes 对齐）

- Client ID: `b1a00492-073a-47ea-816f-4c329264a828`
- Scope: `openid profile email offline_access grok-cli:access api:access`
- Device: `POST https://auth.x.ai/oauth2/device/code`
- Token: discovery 的 `token_endpoint`（通常 `https://auth.x.ai/oauth2/token`）
- API: `https://api.x.ai/v1`
- 参考（只读）：`hermes-agent/hermes_cli/auth.py`、`proxy/server.py`、`proxy/adapters/xai.py`

## 模块边界

- `internal/auth`：登录 / 刷新 / JWT skew / host pin
- `internal/store`：tokens 原子读写 + flock
- `internal/credential`：`GetBearer` / `ForceRefresh` / `Status`
- `internal/proxy`：`net/http` 转发 + path 策略
- 不把 Hermes Python 代码 vendoring 进来

## CLI

```text
xai-proxy [--proxy URL] login [--no-browser] [--proxy URL]
xai-proxy [--proxy URL] serve [--host ...] [--port ...] [--proxy URL]
xai-proxy status
xai-proxy logout
xai-proxy version
```

出站代理（OAuth+API）：`--proxy` / `XAI_PROXY_OUTBOUND` / `ALL_PROXY` / `HTTPS_PROXY` / `HTTP_PROXY`；实现见 `internal/outbound`。

## 明确不做

- 多租户 SaaS / 公网安全网关（无客户端鉴权是本机 DX 设计）
- 冒充 xAI 官方产品
- 依赖 Hermes Python
- Twitter 官方 OAuth API
- OpenAI→xAI TTS/STT/video 假兼容 shim
- 无必要的第三方 HTTP 框架
- WebSocket Voice realtime 网关（当前非目标）

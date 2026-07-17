# xai-proxy

**本机开发者工具** — 单操作者反向代理：通过 OAuth 登录 xAI（Grok）一次后，让任意**本地** OpenAI 兼容客户端调用 xAI 原生 `/v1/*` API（对话 **与** 多模态），而无需自行管理上游 API key。

代理挂上你的 OAuth bearer，并**透传**转发到 `https://api.x.ai/v1/*`。

> **与 xAI 无隶属关系。** 非官方、社区维护软件。  
> **仅供本机使用** — 不是多租户或公网 API 网关。  
> 许可证：[MIT](LICENSE)。安全模型：[docs/SECURITY_zh.md](docs/SECURITY_zh.md)。  
> 英文主文档：[README.md](README.md)。

## 重要：安全与范围

| 应该 | 不要 |
|------|------|
| 绑定 **127.0.0.1**（默认） | 把端口暴露到公网 |
| 每次 `/v1/*` 使用**本地 client API key** | 无 `Authorization` 调用 `/v1` |
| 将 OAuth token **与** generate 得到的 client key 视为密钥 | 提交密钥、`.env` 或 volume 内容 |
| 勿在共享主机上扩散 client key | 像密码一样外传密钥 |

**`/v1/*` 强制本地客户端鉴权**：`Authorization: Bearer <client_key>`。  
`/health`、`/ready` 对探针开放。上游 OAuth 由代理单独挂载。

OAuth 使用**公开**设备码 client id。xAI 可能随时调整白名单或条款；风险自负。

完整威胁模型：[docs/SECURITY_zh.md](docs/SECURITY_zh.md)。

## 适用场景

- 开发时把 **Cursor / OpenAI SDK / curl** 指到本机 base URL
- 一次 OAuth 登录、自动 refresh、全模态 xAI 原生 path

## 不适用

- xAI 官方产品或受支持集成
- 面向团队或公网的安全共享代理
- 完整 OpenAI Audio API shim（`/audio/speech` 等**不会**被映射）

## 环境要求

- Go **1.26.5**
- SuperGrok 或 X Premium+（OAuth API 访问；部分模态可能受上游档位限制）

## 安装

```bash
cd xai-proxy
make build
# 或: go build -o xai-proxy ./cmd/xai-proxy
```

## 使用

```bash
# 生成本地 client API key（完整 key 只在 stdout 打印一次——请立即保存；会覆盖旧 key）
KEY=$(./xai-proxy generate)

./xai-proxy login
./xai-proxy serve   # http://127.0.0.1:7257
```

| 客户端设置 | 值 |
|------------|-----|
| Base URL | `http://127.0.0.1:7257/v1` |
| API Key | **`xai-proxy generate` 得到的本地 key**（只展示一次） |

**没有** `key show`。遗失 key 请再执行 `generate`（旧 key 立即失效）。

## 路径策略

**所有转发路由均为 xAI 原生。** 部分聊天/文本路由与 OpenAI path + body 形状一致（全兼容），OpenAI SDK 可直接使用。对仅部分匹配的路径**不写 shim**（例如不会把 `/audio/speech` 映射到 `/tts`）。

### 支持的 xAI 原生 path（`/v1/...`）

| Path | 能力 | 额外 OpenAI 全兼容 |
|------|------|-------------------|
| `POST /chat/completions` | 对话（如 `grok-4.5`） | 是 |
| `POST /responses` | Responses API | 是 |
| `POST /completions` | Completions | 是 |
| `POST /embeddings` | Embeddings | 是 |
| `GET  /models` | 模型列表 | 是 |
| `POST /images/generations` | 文生图 | 否 |
| `POST /images/edits` | 图编辑（JSON） | 否 |
| `POST /tts` | 文字转语音 | 否 |
| `POST /stt` | 语音转文字（multipart） | 否 |
| `POST /videos/generations` | 视频提交 | 否 |
| `POST /videos/edits` | 视频编辑 | 否 |
| `POST /videos/extensions` | 视频延长 | 否 |
| `GET  /videos/{id}` | 视频任务状态 | 否 |

拒绝（不 shim）：`/audio/speech`、`/audio/transcriptions`、`/audio/translations` → 使用 `/tts` / `/stt`。

Body 上限：**100 MiB**（媒体 / data-URI / STT 上传）。

### 示例

```bash
# KEY 来自: xai-proxy generate  （只展示一次）

# 对话（xAI 原生；也可配合 OpenAI SDK）
curl -s http://127.0.0.1:7257/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}]}'

# 图片
curl -s http://127.0.0.1:7257/v1/images/generations \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-image","prompt":"a red panda coding"}'

# TTS（不是 /audio/speech）
curl -s http://127.0.0.1:7257/v1/tts \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from Grok","voice_id":"Ara","language":"en"}' \
  -o speech.mp3

# STT（不是 /audio/transcriptions）
curl -s http://127.0.0.1:7257/v1/stt \
  -H "Authorization: Bearer $KEY" \
  -F 'file=@./audio.wav' \
  -F 'language=en'

# 视频提交
curl -s http://127.0.0.1:7257/v1/videos/generations \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-video","prompt":"waves on a beach"}'
```

## 存储

| 路径 | 用途 |
|------|------|
| `~/.xai-proxy/tokens.json` | OAuth tokens（`0600`）— **密钥** |
| `~/.xai-proxy/client_key` | 仅保存 client key 的加盐 **SHA-256** 哈希（`0600`）；每次 `generate` 覆盖 |

默认目录为 `~/.xai-proxy`（可用 `XAI_PROXY_HOME` 覆盖）。  
明文 client key **仅**在 `generate` 时展示一次；磁盘为校验串（`v1$sha256$…`），无法还原明文。
切勿提交 token、client key 或 volume 内容。

## 环境变量

### 项目自有

| 变量 | 作用 |
|------|------|
| `XAI_PROXY_HOME` | 配置目录（存放 `tokens.json`、`client_key`；默认 `~/.xai-proxy`） |
| `XAI_PROXY_OUTBOUND` | OAuth + API 的出站 HTTP/SOCKS 代理（优先于标准代理环境变量） |
| `XAI_BASE_URL` | 登录时可选的上游 API base（默认 `https://api.x.ai/v1`；须为 `*.x.ai` 的 HTTPS） |

**没有**通过环境变量注入本地 client API key 的方式（只能 `xai-proxy generate`）。

### 出站代理（标准）

仅影响**出站**（OAuth discovery、设备登录、refresh、API 转发），不是入站客户端代理。

| 变量 | 作用 |
|------|------|
| `XAI_PROXY_OUTBOUND` | 本项目专用代理 URL（环境变量中优先级最高） |
| `ALL_PROXY` / `all_proxy` | 统一代理（常见 SOCKS5） |
| `HTTPS_PROXY` / `https_proxy` | HTTPS 代理 |
| `HTTP_PROXY` / `http_proxy` | HTTP 代理 |
| `NO_PROXY` / `no_proxy` | 不走代理的 host 列表 |

**优先级**（高 → 低）：CLI `--proxy` → `XAI_PROXY_OUTBOUND` → `ALL_PROXY` → `HTTPS_PROXY` → `HTTP_PROXY` → 直连。

协议：`http://`、`https://`、`socks5://`、`socks5h://`（`socks://` → socks5）。

```bash
# 配置目录（可选）
export XAI_PROXY_HOME="$HOME/.xai-proxy"

# SOCKS5 出站（常见本机客户端）
export ALL_PROXY=socks5://127.0.0.1:1080
# 或: export XAI_PROXY_OUTBOUND=socks5://127.0.0.1:1080

./xai-proxy login --no-browser
./xai-proxy serve

# 显式 flag 覆盖 env
./xai-proxy --proxy http://127.0.0.1:7890 serve
./xai-proxy serve --proxy socks5h://127.0.0.1:1080
```

## 命令

```text
xai-proxy generate   # 生成 client key（覆盖；只打印一次）
xai-proxy login [--no-browser]
xai-proxy serve
xai-proxy status | logout | version
```

## 文档

| 文档 | 内容 |
|------|------|
| [docs/SECURITY_zh.md](docs/SECURITY_zh.md) | 范围、威胁模型、报告 |
| [docs/DESIGN_zh.md](docs/DESIGN_zh.md) | 架构、路径、OAuth、CLI |
| [docs/DEPLOY_zh.md](docs/DEPLOY_zh.md) | OCI 镜像构建与运行（Apple Container / Docker） |
| [AGENTS.md](AGENTS.md) | 开发风格与安全准则（仅英文） |

文档与代码注释以**英文**为准；产品类中文文档见并行 `*_zh.md`。`AGENTS.md` 无中文版。

## 免责声明

本软件按 [MIT 许可证](LICENSE)「按现状」提供，无任何担保。你须自行遵守 [xAI](https://x.ai) 服务条款、订阅规则及适用法律。作者不对账号封禁、额度消耗或你发送到上游 API 的数据负责。

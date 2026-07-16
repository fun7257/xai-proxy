# 安全

操作者安全说明、威胁模型与漏洞报告。  
开发风格与编码期安全准则：根目录 [AGENTS.md](../AGENTS.md)（仅英文）。  
架构与鉴权双层模型：[DESIGN_zh.md](DESIGN_zh.md)。  
英文主文档：[SECURITY.md](SECURITY.md)。

## 项目是什么

**xai-proxy** 是**本机开发者工具**：单操作者、机器本地的反向代理，将你的 xAI OAuth 凭证挂到出站 API 请求上。

它**不是**：

- xAI / Grok / SuperGrok 官方产品
- 多租户或公网 SaaS 网关
- 在网络暴露服务前替代外层鉴权的方案

## 预期部署

| 适用 | 不适用 |
|------|--------|
| 工作站上的 `127.0.0.1` / localhost | 无外层鉴权的公网暴露 |
| 单人操作者、一次 OAuth 登录 | 共享局域网 / 不可信多用户访问代理端口 |

**本地客户端鉴权在 `/v1/*` 上是强制的**（`Authorization: Bearer <client_key>`）。持有密钥且能连上端口的人即可使用**你的**订阅额度。`/health`、`/ready` 仍对探针开放。

## 威胁模型

能连上监听端口**并且**持有有效 client key 的人可以：

- 在你的 OAuth 账户上调用对话、图片、TTS/STT、视频等接口
- 消耗 SuperGrok / 订阅额度，触发计费或限流

攻击面 = **代理端口网络可达性** + 本地 client key 持有。无 key 时 `/v1/*` 返回 401。保护密钥文件，优先回环绑定。

## 控制措施

| 控制 | 默认 |
|------|------|
| 监听地址 | 仅 `127.0.0.1`（CLI） |
| 客户端鉴权 | **`/v1/*` 强制**（本地 API key，constant-time 比较） |
| 无 key 开放 | 仅 `/health`、`/ready`（探针） |
| Token 文件 | `~/.xai-proxy/tokens.json` 模式 `0600`，目录 `0700` |
| Client key 文件 | `~/.xai-proxy/client_key` 模式 `0600` — 仅加盐 SHA-256（非明文）；每次 `generate` 覆盖 |
| 上游传输 | 仅 HTTPS |
| Host 钉死 | discovery / token / inference 必须为 `*.x.ai` |
| 日志 | 永不记录 access / refresh token |
| 路径白名单 | 仅已知 xAI 原生 `/v1` 族 |
| Body 大小 | 默认最大 **100 MiB**（媒体）；可调低以缩小暴露面 |

## 非回环绑定

绑定 `0.0.0.0`（或任何非回环地址）需要：

```bash
xai-proxy serve --host 0.0.0.0 --i-understand-non-loopback-bind
```

仅在受信网络、VPN 或**自带鉴权**的反向代理后使用。  
`--i-understand-non-loopback-bind` 只表示允许**非回环监听**；`/v1/*` **始终**需要本地 client API key。

## OAuth 凭证

- 使用**公开** OAuth 设备码客户端（应用内无 client secret）
- Refresh token **单次使用**（轮转）；并发进程使用文件锁
- 终端刷新失败（`invalid_grant`）会隔离本地 token 并要求重新 `login`
- 刷新返回 HTTP **403** 视为**档位/权益拒绝**，不是过期
- 上游策略（允许的客户端、scope、档位）可能随时变更

## 头转发

除 hop-by-hop 与 `Authorization` 外的客户端头会转发到 `api.x.ai`。优先使用简单 API 客户端，避免把完整浏览器指向代理以免带上多余 cookie。

## 出站代理

操作者可通过 `--proxy` 或标准环境变量，将**全部出站**（OAuth + API）走 HTTP 或 SOCKS5。代理 URL 可含凭据，不得记入日志。这不构成入站客户端鉴权。

## 漏洞报告

若认为在**本软件**中发现安全问题（非 xAI 上游 API）：

1. 优先私密报告（GitHub Security Advisory / 维护者邮箱，如已公布）
2. **不要**在公开 issue 中贴 live token、refresh token 或含密钥的完整抓包
3. 包含：受影响版本/commit、复现步骤、影响评估

我们会在合理时间内确认有效报告。无正式漏洞赏金。

## 不在范围内（请勿当作产品缺陷上报）

- 不带 client key 调用 `/v1` 得到 401 — 预期行为
- 操作者绑定 `0.0.0.0` 或将端口发布到回环之外后被滥用
- 上游 xAI 403 / 档位 / 限流
- 使用受 xAI 策略变更影响的第三方 OAuth 公开客户端

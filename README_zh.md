# xLyra

> 面向多上游 AI 服务、AI 中转站和 OAuth 账号的统一控制面与网关。

[English](./README.md) · 简体中文

![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)
![Backend](https://img.shields.io/badge/backend-Go%201.26+-00ADD8.svg)
![Frontend](https://img.shields.io/badge/frontend-React%2019-61DAFB.svg)
![Database](https://img.shields.io/badge/database-PostgreSQL%2017-336791.svg)

xLyra 把分散的中转站、官方模型接口、OAuth 账号和兼容接口收敛到一个控制台中，并向下游应用暴露统一的 OpenAI-style API 入口。它不是单站点反代，而是一个多站点编排层：负责接入、同步、授权、路由、失败转移、用量记录和成本估算。

## 为什么需要 xLyra

| 常见问题 | xLyra 的处理方式 |
| --- | --- |
| 多个中转站和官方账号分散管理 | 将站点、OAuth 账号、上游 Key、模型和价格统一纳入控制台 |
| 下游应用要切换不同 provider 的接口格式 | 提供统一网关，在 Chat、Responses、Messages、Images、Embeddings、Audio 间做协议转换 |
| 不同 Key 能访问的模型和站点不同 | 下游 API Key 支持模型 allowlist、站点 allowlist、站点组授权和模型名映射 |
| 某个上游失败会直接影响业务 | 路由引擎结合健康、延迟、价格、冷却和优先级选择候选，并在可行时 failover |
| 请求成本、失败原因和协议转换不可见 | 记录请求日志、usage、成本估算、上下游路径、错误阶段和流式状态 |

## 系统架构

```mermaid
flowchart LR
  Client[下游应用] --> Gateway[xLyra Gateway]
  Admin[Web 控制台] --> API[Control API]
  API --> DB[(PostgreSQL)]
  Gateway --> Router[路由与授权]
  Router --> DB
  Gateway --> ProviderA[NewAPI / OpenAI Compatible]
  Gateway --> ProviderB[Anthropic / Gemini / DeepSeek 等]
  Gateway --> ProviderC[Codex / Antigravity / Grok / Claude Code / OpenCode Go OAuth]
```

Docker 部署时由三个服务组成：

```text
xlyra-frontend  # nginx 承载 Web 控制台，并代理 /api/v1、/v1、/healthz 等请求
xlyra-backend   # Go 后端，提供控制面 API、网关 API、调度和健康检查
xlyra-postgres  # PostgreSQL 数据库
```

## 网关端点

所有网关端点均在 `/v1` 路径下，需通过 `Authorization: Bearer <key>` 携带下游 API Key。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/v1/models` | 查询下游可见模型 |
| `POST` | `/v1/chat/completions` | OpenAI Chat Completions（流式与非流式） |
| `POST` | `/v1/responses` | OpenAI Responses API |
| `GET` | `/v1/responses` | OpenAI Responses API over WebSocket（`Upgrade: websocket`） |
| `POST` | `/v1/messages` | Anthropic Messages-style 端点 |
| `POST` | `/v1/images/generations` | 图片生成 |
| `POST` | `/v1/images/edits` | 图片编辑（multipart） |
| `POST` | `/v1/embeddings` | Embeddings |
| `POST` | `/v1/audio/speech` | 文字转语音 |

### 协议转换

xLyra 在下游和上游协议格式之间透明转换。Failover 只允许发生在响应第一个字节写出之前；流式响应开始后不会切换上游，以保证下游协议语义完整。

| 下游请求 → | 可路由的上游协议 |
| --- | --- |
| Chat Completions | Chat / Responses / Anthropic Messages / Antigravity |
| Responses | Responses / Chat / Anthropic Messages / Antigravity |
| Messages | Anthropic Messages / Responses / Codex / Chat / Antigravity |
| Images generations | OpenAI Images / Codex 图片生成工具 / Antigravity 图片生成 |
| Images edits | OpenAI multipart / Codex multipart |
| Embeddings | OpenAI-compatible embeddings |
| Audio speech | OpenAI TTS |

其他网关行为：

- **生图工具桥接**：当上游在 Responses 流中无法原生执行 `image_generation` 工具调用时，网关代为发起图片请求，并将结果注入回流式响应。
- **长上下文阶梯计价**：OpenAI 模型输入超 272K tokens 时，自动按官方高档费率计费。
- **模型映射**：下游 API Key 可配置硬映射或软映射（通配兜底）。软映射仅在找不到直接路由时生效。
- **SSE 心跳保活**：流式响应定期发送 SSE 注释，防止长连接被中间代理断开。

## 路由与失败转移

路由选择不只按价格排序，会对每个请求综合评分和过滤候选：

- 站点和模型启用状态
- endpoint type 支持情况
- 可用上游 API Key 数量
- 下游 API Key 的模型与站点授权
- 站点健康、模型成功率、平均延迟和连续失败
- 手工路由优先级和冷却状态

### 冷却机制

瞬态模型冷却（网关触发的可恢复失败）采用半开策略：候选仍保留在池中，但排在所有健康候选之后。成功一次后立即解冷。

| 触发原因 | 初始时长 | 升级规则 |
| --- | --- | --- |
| 可恢复上游错误 | 30 秒 | 30 分钟内每次激活翻倍，上限 5 分钟 |
| 401 凭证错误 | 5 分钟 | 窗口内重复触发升至 30 分钟 |
| 429 带 Retry-After | 跟随 Retry-After（5 秒–2 分钟区间限制） | 无头部时默认 30 秒 |
| 上游流式中断 | 连续 3 次流式失败后触发冷却 | 与无响应连续失败门限一致 |

并发队列超时（`upstream_concurrency_wait_timeout`）不触发冷却。网关还支持按模型配置 RPM 上限，具备内存队列和上游 429 等待重试能力。

## 支持的站点类型

| 类型 | 当前能力 |
| --- | --- |
| OpenAI Compatible / 官方 API Key 类站点 | 凭证校验、模型同步、协议转发 |
| Anthropic | Messages 协议转发，支持 Chat / Responses ↔ Messages 转换 |
| Grok | OAuth CLI 通道、多账号管理、图片生成、按 tier 分档模型可用性 |
| OpenCode Go | 订阅套餐模型与额度路由 |
| DeepSeek / Minimax / Moonshot / Kimi Code / 小米 MiMo / Google Gemini | 按站型适配凭证、模型同步或兼容协议转发 |
| GLM（智谱） | 凭证校验、模型同步、兼容协议转发 |
| xLyra | 级联中转：带 ETag 缓存的模型同步、下游 API Key 清单与摘要、管理员用户摘要 |
| NewAPI | 站点探测、用户摘要、API Key 清单、Key 摘要、签到、模型和价格同步 |
| Codex | OAuth、ChatGPT 账号额度、模型同步、Responses 协议和图片生成工具转换 |
| Antigravity | OAuth、项目/配额抓取，文本与图片请求到 Gemini-style 协议转换 |
| Claude Code | OAuth 授权和客户端伪装转发 |

## 控制台功能

### 站点与凭证

- 站点创建、编辑、启停、删除、校验和刷新
- 站点 API Key 管理：新增、更新、轮换和单 Key 刷新（含错误隔离）
- Codex / Antigravity / Grok / Claude Code OAuth 授权、刷新、导入、模型同步和额度同步
- OAuth 重置卡管理与兑换
- 上游凭证余额探测：按凭证展示余额与已用量

### 模型与价格

- 模型广场、标准模型、alias、站点模型绑定和支持矩阵
- 手动标准模型价格，自动作为全局兜底向所有站点传播
- LiteLLM 和 models.dev 自动价格同步；手动价格不被自动同步覆盖
- 站点级和站点组价格倍率
- OpenAI 模型长上下文阶梯计价

### 下游 API Key

- Key 管理、额度、过期时间、模型权限、站点权限和站点组授权
- 模型名映射规则（硬映射和软映射/通配兜底）
- 日/周额度周期与自动重置
- 每个 Key 的用量门户页（无需鉴权，可公开访问）

### 路由与健康

- 路由候选、当前路由、trace、健康状态和冷却记录
- 手动路由选择、失败转移和冷却管理
- 站点健康历史、小时级明细和主动健康检查

### 可观测性

- 请求日志：筛选、分页、完整请求详情、用量和成本估算
- 用量分账统计：按下游 Key、模型、站点聚合拆分
- 仪表盘：RPM、Token 吞吐、成本、活跃 Key 热力图和冷却摘要
- 实时请求流图：在途请求以节点连线拓扑形式实时可视化
- 模型体验工作台：多协议交互测试（Chat、Responses、Messages），支持附件对话

### 系统

- 管理员登录、资料、密码、TOTP、会话、Access Token 和审计日志
- 备份与恢复：手动导出/导入，以及 S3 兼容的自动定期备份
- 系统代理配置与连通性测试

## 快速开始

前置要求：

- Docker
- Docker Compose

启动：

```bash
docker compose up -d --build
```

默认控制台：

```text
http://localhost:5800
```

首次访问控制台时创建第一个管理员账号。后续登录使用服务端 Session Cookie。

停止：

```bash
docker compose down
```

删除持久化数据：

```bash
docker compose down -v
```

公网或多人共享部署前，请至少修改 `docker-compose.yml` 中的默认 PostgreSQL 密码，并配置 HTTPS、反向代理、CORS 来源和 IP 白名单。应用加密密钥会在首次启动时自动生成，并持久化到 `./data/conf/master.key`。

## 常用配置

Docker 默认数据挂载：

```text
./postgres  -> PostgreSQL 数据
./data      -> 后端运行数据、配置和 master.key
```

常用环境变量：

| 变量 | 说明 |
| --- | --- |
| `APP_ENV` | 运行环境，支持 `development`、`test`、`staging`、`production` |
| `HTTP_PORT` | 后端监听端口，默认 `5801` |
| `POSTGRES_DSN` | PostgreSQL DSN；设置后优先于拆分式数据库配置 |
| `DB_HOST` / `DB_PORT` / `DB_NAME` | 数据库地址、端口和库名 |
| `DB_USER` / `DB_PASSWORD` | 数据库用户名和密码 |

## 本地开发

后端：

```bash
cd server
go run ./cmd/server
```

前端：

```bash
cd web
pnpm install
pnpm dev
```

常用命令：

```bash
make web-install
make web-dev
make web-build
make server-run
make server-build
make docker-up
make docker-down
```

验证：

```bash
cd web
pnpm lint
pnpm typecheck
pnpm build
```

```bash
cd server
go test ./...
go build ./cmd/server
```

## 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go、net/http、chi、GORM、PostgreSQL、goose、robfig/cron、slog |
| 前端 | TypeScript、React、Vite、React Router、Tailwind CSS、TanStack Query、TanStack Table、Zustand、Recharts、i18next |
| 部署 | Docker、Docker Compose、nginx |

## License

xLyra 使用 [GNU Affero General Public License v3.0](./LICENSE)。

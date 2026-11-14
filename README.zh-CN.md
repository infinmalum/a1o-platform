<div align="center">
  <img src="docs/logo.png" alt="A1O Platform 徽标" width="120" />

  # A1O Platform

  **一个平台原生支持 OpenAI、Anthropic、Gemini 三种协议，连接所有模型服务商。**

  [![CI](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
  ![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
  ![Node.js](https://img.shields.io/badge/Node.js-24-339933?logo=nodedotjs&logoColor=white)

  [English](README.md) · 简体中文 · [日本語](README.ja.md)
</div>

---

A1O Platform 是一个独立维护的 AI 网关，最初派生自 [One API](https://github.com/songquanpeng/one-api)。它以一套部署原生支持 OpenAI（Chat Completions 与 Responses）、Anthropic Messages、Gemini generateContent 三种 API 协议，连接多个模型服务，并提供渠道、用户、令牌、额度和用量日志的管理界面。One API 由 JustSong 及其他贡献者创建；A1O Platform 建立在他们的工作之上。

> [!NOTE]
> 本仓库独立维护。原项目的版本、Docker 镜像、演示站点和 issue 列表不代表 A1O Platform 的发布与支持渠道。

## 功能亮点

- 🔌 **四种原生 API 协议** —— OpenAI Chat Completions、OpenAI Responses（支持可查询、可取消的后台任务）、Anthropic Messages、Gemini `generateContent`，同一个令牌还能调用 embeddings、图像、音频和内容审核接口。
- 🌉 **跨协议桥接** —— 请求与响应会在客户端协议和上游协议之间自动转换，流式同样支持：OpenAI 客户端可以打到 Anthropic、Gemini、Responses 上游，Anthropic 客户端可以打到 OpenAI chat 和 Gemini 上游。
- 🔀 **智能路由** —— 15 种渠道类型、9 个服务商适配器，渠道负载均衡、模型映射、分组倍率、OAuth 连接的订阅账号（ChatGPT 套餐）、订阅优先的路由策略，以及首字节返回前的自动重试。
- 🤖 **内置 chat 运行时** —— Chat 和 Work 共用 [Lapua](https://github.com/infinmalum/Lapua) 工具循环，支持持久会话、沙箱化的 Python/Node 代码执行（rlimits 资源限制 + 网络隔离），以及 MCP（Model Context Protocol）工具服务器。
- 🧮 **额度与计费** —— 令牌额度、用量日志、兑换码、充值，每个请求都按倍率精确结算。
- 🖥️ **双前端** —— `/` 的 Studio 提供聊天、后台任务、智能体广场、翻译和图像生成。`/admin/` 的控制台管理渠道、上游账号、用户、令牌、日志和设置。
- 💾 **灵活存储** —— 开箱即用 SQLite；共享存储支持 MySQL / PostgreSQL；缓存与同步可配合 Redis；支持 master/slave 多节点部署。
- 🛠️ **现代技术栈** —— Go 后端搭配 TypeScript + Vite 前端，每次前端构建都会先做类型检查。

不同渠道对模型和功能的支持并不完全相同。正式使用前，请在渠道设置中确认并测试目标模型。

## 使用 Docker 快速启动

在本仓库根目录构建本项目的镜像：

```sh
git clone https://github.com/infinmalum/a1o-platform.git
cd a1o-platform
docker build -t a1o-platform:local .
docker run -d --name a1o-platform --restart unless-stopped -p 3000:3000 -v "$(pwd)/data:/data" a1o-platform:local
```

首次启动后打开 <http://localhost:3000/admin/setup> 创建 root 账号。无人值守部署时，可在首次启动前给容器传入 `INITIAL_ROOT_PASSWORD`。初始化后，从 <http://localhost:3000/> 打开 Studio，从 <http://localhost:3000/admin/> 打开管理控制台。更换容器时保留数据卷。对外提供服务时，配置 HTTPS 和固定的 `SESSION_SECRET`。

仓库的 `docker-compose.yml` 会在本地构建本项目，镜像标记为 `a1o-platform:local`，数据库与数据目录均使用 `a1o-platform` 命名。如果你沿用了早期继承 `one-api` 命名的 compose 文件，切换前需要先迁移数据库和 `data/` 目录。

## 从源码构建

准备 Node.js 24、Go 1.27.1，以及 CGO SQLite 驱动所需的 C 编译器。在仓库根目录运行以下命令。

```sh
npm ci --legacy-peer-deps --prefix web/studio
npm run build --prefix web/studio
npm ci --legacy-peer-deps --prefix web/admin
npm run build --prefix web/admin
go build -o a1o-platform .
./a1o-platform --port 3000
```

Vite 将构建产物分别写入 `web/build/studio` 和 `web/build/admin`，Go 二进制会嵌入两个前端。Lapua 驱动 Chat 和 Work 的模型与工具循环。A1O 提供已获授权的工具，负责模型路由、计费、持久化和调度。

## 配置与使用

启动服务前可设置环境变量。常用配置如下：

| 环境变量 | 用途 |
| --- | --- |
| `PORT` | HTTP 监听端口，默认 `3000`。 |
| `SQL_DSN` | MySQL DSN 或 `postgres://` 地址；不设置时使用 SQLite。 |
| `SQLITE_PATH` | 使用 SQLite 时的数据库文件路径。 |
| `SESSION_SECRET` | 固定会话密钥；重启或多节点部署时尤其需要。 |
| `REDIS_CONN_STRING` | Redis 连接地址，用于缓存。 |
| `SYNC_FREQUENCY` | 缓存同步间隔，单位为秒。 |
| `NODE_TYPE` | 多节点部署时的 `master` 或 `slave`。 |
| `FRONTEND_BASE_URL` | 从节点可选的前端跳转地址。 |

多节点部署时，所有节点应连接同一个 MySQL 或 PostgreSQL 数据库，使用相同的 `SESSION_SECRET`，并配置节点角色与缓存同步。具体可用配置以 [`common/config`](common/config) 中的代码为准。

登录后，在 **渠道** 页面填写模型服务商密钥，再在 **令牌** 页面创建访问令牌。客户端按各自协议访问对应端点，同一令牌即可认证所有端点：

| 客户端协议 | 端点 |
| --- | --- |
| OpenAI Chat Completions | `POST /v1/chat/completions` |
| OpenAI Responses | `POST /v1/responses`（后台任务：`GET /v1/responses/{id}`、`POST /v1/responses/{id}/cancel`） |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `POST /v1beta/models/{模型名}:generateContent` 或 `:streamGenerateContent`（`/v1` 下同样可用） |
| OpenAI Embeddings | `POST /v1/embeddings` |
| OpenAI 图像 / 音频 / 审核 | `POST /v1/images/generations`、`/v1/audio/speech`、`/v1/audio/transcriptions`、`/v1/audio/translations`、`/v1/moderations` |
| SystemOne 评测 | `POST /v1/systemone` |

上游渠道不必与客户端使用同一协议：网关会在两者之间转换。例如 Anthropic 协议的客户端可以由 OpenAI chat 或 Gemini 渠道服务，OpenAI Chat 请求也能通过 Responses 桥接跑在 ChatGPT 订阅账号上。

OpenAI 兼容客户端将地址设为 `http://localhost:3000/v1`，并使用刚创建的令牌作为 API Key：

```sh
curl http://localhost:3000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello!"}]}'
```

## 文档

- [前端开发说明](web/README.md)

## 开发

- 修改前端时，运行对应应用的 `npm run typecheck --prefix web/studio` 或 `npm run typecheck --prefix web/admin`。编译包含最新前端资源的 Go 程序前，先构建两个应用。
- 修改后端时，运行 `go test ./...`。

请在[本仓库的 issue 列表](https://github.com/infinmalum/a1o-platform/issues)反馈问题与改动。

## 来源与许可证

A1O Platform 派生自 [One API](https://github.com/songquanpeng/one-api)，原项目版权归 © 2023 JustSong 及其他贡献者所有。原项目的 MIT 许可及声明保存在 [`LICENSE.upstream`](LICENSE.upstream)。本派生项目的原创修改按 [Apache License 2.0](LICENSE) 提供；归属与适用范围见 [`NOTICE`](NOTICE)。各源码文件及第三方依赖原有的许可声明仍然适用。

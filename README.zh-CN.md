<div align="center">
  <img src="docs/logo.png" alt="A1O Platform 徽标" width="120" />

  # A1O Platform

  **一个平台原生支持 OpenAI、Anthropic、Gemini 三种协议，连接所有模型服务商。**

  [![CI](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-AGPL_v3.0-blue.svg)](LICENSE)
  ![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
  ![Node.js](https://img.shields.io/badge/Node.js-24-339933?logo=nodedotjs&logoColor=white)

  [English](README.md) · 简体中文 · [日本語](README.ja.md) · [Français](README.fr.md) · [Русский](README.ru.md) · [Español](README.es.md)
</div>

---

A1O Platform 是一个独立维护的 AI 网关，最初派生自 [One API](https://github.com/songquanpeng/one-api)。它以一套部署原生支持 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini `generateContent` 四种 API 格式，连接多个模型服务，并提供渠道、用户、令牌、额度和用量日志的管理界面。One API 由 JustSong 及其他贡献者创建；A1O Platform 建立在他们的工作之上。

> [!NOTE]
> 本仓库独立维护。原项目的版本、Docker 镜像、演示站点和 issue 列表不代表 A1O Platform 的发布与支持渠道。

## 功能亮点

- 🔌 **四种原生 API 协议** —— OpenAI Chat Completions、OpenAI Responses（支持可查询、可取消的后台任务）、Anthropic Messages、Gemini `generateContent`，同一个令牌还能调用 embeddings、图像、音频和内容审核接口。
- 🌉 **跨协议桥接** —— 请求与响应会在客户端协议和上游协议之间自动转换，流式同样支持：OpenAI 客户端可以打到 Anthropic、Gemini、Responses 上游，Anthropic 客户端可以打到 OpenAI chat 和 Gemini 上游。
- 🔀 **智能路由** —— 渠道负载均衡、模型映射、分组倍率、OAuth 连接的订阅账号（ChatGPT 套餐、Claude Code、Gemini CLI）、订阅优先的路由策略，以及首字节返回前的自动重试。
- 🤖 **内置 chat 运行时** —— Chat 和 Work 共用 [Norma](https://github.com/infinmalum/norma-e) 工具循环，支持持久会话、沙箱化的 Python/Node 代码执行（rlimits 资源限制 + 网络隔离），以及 MCP（Model Context Protocol）工具服务器。
- 🧮 **额度与计费** —— 令牌额度、用量日志、兑换码、充值，每个请求都按倍率精确结算。
- 🖥️ **双前端** —— `/` 的 Studio 提供聊天与多协议 Playground、后台任务、智能体广场、私有资料库，以及图像、视频和音频工具。`/admin/` 的控制台管理渠道、上游账号、用户、令牌、日志和设置。
- 💾 **灵活存储** —— Docker Compose 默认使用 PostgreSQL，也支持 SQLite 和 MySQL；缓存与同步可配合 Redis；支持 master/slave 多节点部署。
- 🛠️ **现代技术栈** —— Go 后端搭配 TypeScript + Vite 前端，每次前端构建都会先做类型检查。

不同渠道对模型和功能的支持并不完全相同。正式使用前，请在渠道设置中确认并测试目标模型。

## 使用 Studio

先在管理控制台配置服务商渠道，并为用户分组启用所需模型。登录 Studio 后，在对应页面选择模型。Playground 和媒体工具沿用 Studio 登录会话，模型请求从当前账户额度结算。

| 入口 | 用途 |
| --- | --- |
| **对话 → Playground** | 发送原生 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 或 Gemini 请求。查看原始 JSON 或流式事件，用同一份请求对比两个模型，并复制 cURL 示例。 |
| **对话 → 工具与 Skill** | 创建个人 Skill，或导入一份不超过 64 KiB 的 `SKILL.md` 并编辑后保存。在对话里选择 Skill，或将它绑定到智能体供对话和 Work 使用。支持工具调用的模型会看到名称和描述，并按需加载指令。 |
| **工作区 → 资料库** | 上传私有 UTF-8 TXT 或 Markdown 文件，在对话中选择资料库，并打开回复引用的原文。关键词检索使用现有数据库，无需嵌入模型或向量库。单份文件上限为 256 KiB，每库最多 64 份且总量不超过 2 MiB。 |
| **图像生成 → 图片／视频** | 通过火山引擎方舟 Seedream、兼容的 Gemini 等已支持模型生成图片，可预览、下载原图，并将图片附到对话或 Work。Seedream 和兼容的 Gemini 图像模型支持 PNG、JPEG 和 WebP 参考图。方舟 Seedance 支持文生视频及可选首帧，输出 720p、5–8 秒视频。Gemini `gemini-omni-1.2-flash` 支持生成 360p、3–8 秒的文生视频。 |
| **音频** | 使用兼容模型合成语音、转写音频或将语音翻译成英文。可试听、下载生成的音频，也可导出转写文本。 |
| **账户 → 兑换码** | 使用兑换码，或通过站点已启用的 Stripe Checkout、Airwallex、易支付 V1/V2、NOWPayments 或 GMPay Edge 购买一次性额度套餐。 |

站长通过 `ARK_MEDIA_PRICES` 设置方舟模型额度费率，通过 `PAYMENT_PACKAGES` 配置套餐价格。Stripe、Airwallex、NOWPayments 和 GMPay 使用美元美分。易支付的人民币价格以分单独配置。用 `NOWPAYMENTS_PAY_CURRENCIES` 启用一种或多种 NOWPayments 币种和网络，在 GMPay Edge 中配置 GMPay 收款方式。接入和测试步骤见[媒体与支付部署指南](docs/media-and-payments.md)。

对话输入框里的 Skill 选择从下一条消息生效。如需修改会话默认选择，请在对话设置中保存。全局 Skill 由管理员在控制台管理。导入读取 YAML 中的 `name`、`description` 和 Markdown 正文。工具白名单在 Skill 编辑器中配置。导入只处理指令文本，不会安装脚本或连接 MCP 服务。

Studio 顶栏提供 10 种界面语言，阿拉伯语和波斯语支持从右到左布局。管理控制台独立保存中英文偏好。

单实例可直接使用 SQLite，无需 Redis 或向量数据库。备份或迁移时，请保留数据库和数据卷。对话附件以及生成的图片、视频都保存在磁盘上。

## 使用 Docker 快速启动

新部署请在已安装 Docker Compose 和 OpenSSL 的环境中运行以下命令。

```sh
git clone https://github.com/infinmalum/a1o-platform.git
cd a1o-platform
umask 077
cp .env.example .env
printf 'POSTGRES_PASSWORD=%s\nSESSION_SECRET=%s\n' "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" >> .env
docker compose up -d
```

首次启动后打开 <http://localhost:3000/admin/setup> 创建 root 账号。无人值守部署时，可在首次启动前给容器传入 `INITIAL_ROOT_PASSWORD`。初始化后，从 <http://localhost:3000/> 打开 Studio，从 <http://localhost:3000/admin/> 打开管理控制台。更换容器时保留数据卷。对外提供服务时，配置 HTTPS 和固定的 `SESSION_SECRET`。

默认 Compose 部署使用 PostgreSQL，数据保存在 `./data/postgres`。如需 SQLite 或连接已有 PostgreSQL、MySQL，使用 `docker compose -f docker-compose.external.yml up -d`，外部数据库通过 `SQL_DSN` 配置。Redis 可选。

升级原来内置 MySQL 的站点时，保留原项目目录与凭据，并运行 `docker compose -f docker-compose.mysql.yml up -d`。已有站点改用其他数据库前，先备份、迁移并验证原有数据。切换和回退步骤见[数据库部署指南](docs/database-deployment.md)。

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

Vite 将构建产物分别写入 `web/build/studio` 和 `web/build/admin`，Go 二进制会嵌入两个前端。Norma 驱动 Chat 和 Work 的模型与工具循环。A1O 提供已获授权的工具，负责模型路由、计费、持久化和调度。

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
- [账号隐私、长期记忆和数据删除](docs/privacy.md)

## 开发

- 修改前端时，运行对应应用的 `npm run typecheck --prefix web/studio` 或 `npm run typecheck --prefix web/admin`。编译包含最新前端资源的 Go 程序前，先构建两个应用。
- 修改后端时，运行 `go test ./...`。

请在[本仓库的 issue 列表](https://github.com/infinmalum/a1o-platform/issues)反馈问题与改动。

## 来源与许可证

A1O Platform 派生自 [One API](https://github.com/songquanpeng/one-api)，原项目版权归 © 2023 JustSong 及其他贡献者所有。原项目的 MIT 许可及声明保存在 [`LICENSE.upstream`](LICENSE.upstream)。本派生项目的原创修改按 [GNU Affero 通用公共许可证 v3.0（AGPL-3.0）](LICENSE) 提供；归属与适用范围见 [`NOTICE`](NOTICE)。各源码文件及第三方依赖原有的许可声明仍然适用。

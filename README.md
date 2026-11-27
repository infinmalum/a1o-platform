<div align="center">
  <img src="docs/logo.png" alt="A1O Platform logo" width="120" />

  # A1O Platform

  **One platform that speaks OpenAI, Anthropic, and Gemini — for every model provider.**

  [![CI](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-AGPL_v3.0-blue.svg)](LICENSE)
  ![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
  ![Node.js](https://img.shields.io/badge/Node.js-24-339933?logo=nodedotjs&logoColor=white)

  English · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Français](README.fr.md) · [Русский](README.ru.md) · [Español](README.es.md)
</div>

---

A1O Platform is an independently maintained AI gateway, originally derived from [One API](https://github.com/songquanpeng/one-api). It fronts many model providers through one deployment that natively speaks the OpenAI (Chat Completions and Responses), Anthropic, and Gemini API formats, and ships Studio for end users and an administration console for channels, users, tokens, quotas, and usage logs. One API was created by JustSong and its contributors; A1O Platform builds on their work.

> [!NOTE]
> This repository is maintained independently of One API. One API's releases, Docker images, demo sites, and issue trackers are not releases or support channels for A1O Platform.

## Highlights

- 🔌 **Four native API formats** — OpenAI Chat Completions, OpenAI Responses (including background runs you can poll or cancel), Anthropic Messages, and Gemini `generateContent`, plus embeddings, images, audio, and moderations endpoints under the same token.
- 🌉 **Cross-protocol bridging** — requests and responses are converted between client and upstream formats, streaming included: OpenAI clients reach Anthropic, Gemini, and Responses upstreams, and Anthropic clients reach OpenAI-chat and Gemini upstreams.
- 🔀 **Smart routing** — channel load balancing, model mappings, group rates, OAuth-connected subscription accounts (ChatGPT plans, Claude Code, Gemini CLI), subscription-first routing policies, and automatic retries before the first byte reaches the client.
- 🤖 **Built-in chat runtime** — a [Norma](https://github.com/infinmalum/norma-e) tool loop shared by Chat and Work, with persistent sessions, sandboxed Python and Node code execution (rlimits plus network isolation), and Model Context Protocol (MCP) tool servers.
- 🧮 **Quota and billing** — per-token quotas, usage logs, redemption codes, top-up, and rate-based settlement for every request.
- 🖥️ **Web apps** — Studio at `/` includes Chat with a multi-protocol Playground, background Work tasks, an agent library, private document libraries, and image, video, and audio tools. The console at `/admin/` manages channels, upstream accounts, users, tokens, logs, and settings.
- 💾 **Flexible storage** — PostgreSQL by default in Docker Compose; SQLite and MySQL are also supported; Redis for caching and synchronization; master/slave multi-node deployments.
- 🛠️ **Modern stack** — a Go backend with TypeScript and Vite frontends; every frontend build runs its type check first.

Provider and model support varies by channel implementation. Check the channel settings and test a model before relying on it in production.

## Using Studio

Configure a provider channel and enable the model for your group in the admin console. Sign in to Studio and select the model for the task. Playground and the media tools use your Studio session, and model calls draw from your account quota.

| Open | What you can do |
| --- | --- |
| **Chat → Playground** | Send native OpenAI Chat Completions, OpenAI Responses, Anthropic Messages, or Gemini requests. Inspect raw JSON or streaming events, compare two models with the same request, and copy a cURL example. |
| **Chat → Tools & Skills** | Create a personal Skill or import a single `SKILL.md` up to 64 KiB into an editable form. Select Skills in Chat or bind them to an agent for Chat or Work. A tool-capable model sees their names and descriptions and loads instructions as needed. |
| **Workspace → Knowledge libraries** | Upload private TXT, Markdown, CSV, TSV, PDF, DOCX, XLSX, PPTX, JPEG, PNG, GIF or WebP files. Chat searches extracted text and links to cited passages. Default limits are 16 MiB per file and 256 MiB per library, with up to 64 files. An administrator can change both size limits. See the [knowledge library guide](docs/knowledge-libraries.md). |
| **Image generation → Image / Video** | Generate images with supported models, including Volcengine Ark Seedream and compatible Gemini models. Preview or download originals and attach images to Chat or Work. Seedream and compatible Gemini image models accept PNG, JPEG, and WebP references. Seedance on Ark generates 720p clips of 5–8 seconds from text with an optional first frame. Gemini `gemini-omni-1.2-flash` generates 360p text-to-video clips of 3–8 seconds. |
| **Audio** | Select a compatible model to generate speech, transcribe an audio file, or translate speech into English. Play and download generated audio or export the transcript. |
| **Account → Redeem** | Redeem a code or buy a one-time quota package through Stripe Checkout, Airwallex, EPay V1/V2, NOWPayments, or GMPay Edge when enabled. |

The operator sets Ark model rates with `ARK_MEDIA_PRICES` and defines package prices with `PAYMENT_PACKAGES`. Stripe, Airwallex, NOWPayments, and GMPay use USD cents. EPay uses a separate CNY price in fen. Set `NOWPAYMENTS_PAY_CURRENCIES` to enable one or more NOWPayments assets and networks. Configure GMPay receiving methods in GMPay Edge. The [media and payments guide](docs/media-and-payments.md) covers provider setup and testing.

The Chat composer selection takes effect on the next message. Save the selection in Chat settings to change the session defaults. Administrators manage global Skills in the admin console. Import reads the YAML `name` and `description` and the Markdown body. Configure allowed tools in the Skill editor. Imports contain instructions only and do not install scripts or connect MCP servers.

The Studio header lets you choose from ten interface languages, including right-to-left layouts for Arabic and Persian. The admin console has its own Chinese and English preference.

A single instance can use SQLite without running Redis or a vector database. Keep the database and the data volume when backing up or moving the deployment. Chat attachments and generated image and video files are stored on disk.

The signed-in Model Catalog shows enabled model names, their access groups, and a category inferred from each model name. It starts with models available to your group. Browse all models or filter by a specific group or category. When configured, token prices are shown for chat models available to you.

## Quick start with Docker

The default instructions build the application from source. For a prebuilt Linux amd64 image, see the [GHCR deployment instructions](docs/database-deployment.md#deploy-a-prebuilt-image-from-ghcr).

For a new installation, run the following from a shell with Docker Compose and OpenSSL.

```sh
git clone https://github.com/infinmalum/a1o-platform.git
cd a1o-platform
umask 077
cp .env.example .env
printf 'POSTGRES_PASSWORD=%s\nSESSION_SECRET=%s\n' "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" >> .env
docker compose up -d
```

Open <http://localhost:3000/admin/setup> on first launch to create the root account. For unattended setup, pass `INITIAL_ROOT_PASSWORD` to the container before its first start. Once initialized, open Studio at <http://localhost:3000/> or the admin console at <http://localhost:3000/admin/>. Keep the data volume when replacing the container. For a public deployment, configure HTTPS and a persistent `SESSION_SECRET`.

The default Compose deployment starts PostgreSQL and stores its data in `./data/postgres`. For SQLite or an existing PostgreSQL or MySQL database, use `docker compose -f docker-compose.external.yml up -d` and set `SQL_DSN` for external databases. Redis is optional.

If upgrading the previous bundled MySQL deployment, keep the same project directory and credentials and run `docker compose -f docker-compose.mysql.yml up -d`. Back up and transfer existing data before changing databases. See the [database deployment guide](docs/database-deployment.md) for the checks and rollback procedure.

## Build from source

Use Node.js 24, Go 1.27.1, and a C compiler for the CGO SQLite driver. Run these commands from the repository root.

```sh
npm ci --legacy-peer-deps --prefix web/studio
npm run build --prefix web/studio
npm ci --legacy-peer-deps --prefix web/admin
npm run build --prefix web/admin
go build -o a1o-platform .
./a1o-platform --port 3000
```

Vite writes the two apps to `web/build/studio` and `web/build/admin`. The Go binary embeds both builds. Norma drives the model and tool loop in both Chat and Work. A1O supplies the permitted tools and owns model routing, billing, persistence, and scheduling.

## Configure and use

Set environment variables before starting the server. Common settings are:

| Variable | Purpose |
| --- | --- |
| `PORT` | HTTP listening port; default `3000`. |
| `SQL_DSN` | MySQL DSN or `postgres://` URL. If unset, the server uses SQLite. |
| `SQLITE_PATH` | SQLite database path when using SQLite. |
| `SESSION_SECRET` | Stable secret for sessions, especially across restarts or replicas. |
| `REDIS_CONN_STRING` | Redis connection URL for cache use. |
| `SYNC_FREQUENCY` | Cache synchronization interval in seconds. |
| `NODE_TYPE` | `master` or `slave` for multi-node deployments. |
| `FRONTEND_BASE_URL` | Optional frontend redirect for a slave node. |

For multiple instances, point every node at the same MySQL or PostgreSQL database, use the same `SESSION_SECRET`, and configure node roles and cache synchronization. The source of truth for supported settings is the [`common/config`](common/config) package.

After signing in, add a provider key on **Channels**, then create an access token on **Tokens**. Clients speak their native format at the matching endpoint, and the same token authenticates all of them:

| Client format | Endpoint |
| --- | --- |
| OpenAI Chat Completions | `POST /v1/chat/completions` |
| OpenAI Responses | `POST /v1/responses` (background runs: `GET /v1/responses/{id}`, `POST /v1/responses/{id}/cancel`) |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `POST /v1beta/models/{model}:generateContent` or `:streamGenerateContent` (also mounted under `/v1`) |
| OpenAI Embeddings | `POST /v1/embeddings` |
| OpenAI images / audio / moderations | `POST /v1/images/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions`, `/v1/audio/translations`, `/v1/moderations` |
| SystemOne evaluation | `POST /v1/systemone` |

The upstream channel does not have to speak the client's format: the gateway bridges between protocols, so an Anthropic-format client can be served by an OpenAI-chat or Gemini channel, and OpenAI Chat requests can run on ChatGPT subscription accounts through Responses bridging.

An OpenAI-compatible client points at `http://localhost:3000/v1` and uses that token as its API key:

```sh
curl http://localhost:3000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello!"}]}'
```

### Prompt caching and protocol conversion

- In **Channels**, enable five-minute prompt caching on a channel that accepts bridged Claude requests when the upstream model supports it. The setting is off by default. Bedrock and Vertex Claude use block-level cache markers. Chat Completions text blocks may carry an explicit Claude `cache_control` marker. Native Anthropic Messages clients control caching in their own requests.
- In **Operation Settings → Model Prices**, set separate prices per million tokens for cache reads, cache writes, and one-hour cache writes. Blank read and write fields inherit the input price. A blank one-hour write price inherits the default write price. A price of zero explicitly makes that category free. Fetching OpenRouter models fills missing cache prices when the catalog publishes them and preserves configured prices. The consume log reports total input alongside the cache token breakdown.
- OpenAI-format requests converted to Claude Messages default to 4,096 output tokens when the caller leaves the limit unspecified. Operators can set a different default on the channel. An explicit caller limit takes precedence, and the gateway uses the effective limit for the provisional charge.
- Chat and Work use the session or task when choosing between eligible channels of the same priority. API chat requests use an explicit `prompt_cache_key` or the first user message. Use a consistent key on requests that no longer carry the original first message. Disabled or unavailable channels can be bypassed according to the existing routing policy.
- When Chat compacts its history, the summary request reuses the session's instructions, tool definitions, and the conversation prefix being compacted. The summary instruction follows that prefix. Cache hits depend on the upstream and which parts of the prefix are still cached.
- Chat Completions streaming through Claude or Gemini supports `stream_options.include_usage`. The Chat `user` hint is accepted on these bridges and stays out of the upstream request. Anthropic Messages cache hints are dropped when the request is converted to OpenAI or Gemini, whose cache configuration has a different format. With tools enabled, bridges to Gemini and Bedrock Converse reject `parallel_tool_calls: false` because those target wires cannot enforce it.

## Documentation

- [Frontend development](web/README.md)
- [Knowledge library formats, limits, and deployment](docs/knowledge-libraries.md)
- [Account privacy, memory, and data deletion](docs/privacy.md)

## Development

- Run `npm run typecheck --prefix web/studio` or `npm run typecheck --prefix web/admin` for the app you are editing. Rebuild both apps before compiling a binary with the latest frontend assets.
- Run `go test ./...` for backend changes.

Report bugs and changes in [this repository's issues](https://github.com/infinmalum/a1o-platform/issues).

## Origin and licenses

A1O Platform is derived from [One API](https://github.com/songquanpeng/one-api), copyright © 2023 JustSong and other contributors. The upstream MIT license and notice are preserved in [`LICENSE.upstream`](LICENSE.upstream). Original changes made in A1O Platform are offered under the [GNU Affero General Public License v3.0](LICENSE); see [`NOTICE`](NOTICE) for attribution and scope. Existing per-file and third-party license notices remain applicable.

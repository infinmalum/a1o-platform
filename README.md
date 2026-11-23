<div align="center">
  <img src="docs/logo.png" alt="A1O Platform logo" width="120" />

  # A1O Platform

  **One platform that speaks OpenAI, Anthropic, and Gemini — for every model provider.**

  [![CI](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
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
- 🔀 **Smart routing** — channel load balancing, model mappings, group rates, OAuth-connected subscription accounts (ChatGPT plans), subscription-first routing policies, and automatic retries before the first byte reaches the client.
- 🤖 **Built-in chat runtime** — a [Lapua](https://github.com/infinmalum/Lapua) tool loop shared by Chat and Work, with persistent sessions, sandboxed Python and Node code execution (rlimits plus network isolation), and Model Context Protocol (MCP) tool servers.
- 🧮 **Quota and billing** — per-token quotas, usage logs, redemption codes, top-up, and rate-based settlement for every request.
- 🖥️ **Web apps** — Studio at `/` includes Chat with a multi-protocol Playground, background Work tasks, an agent library, private document libraries, and image, video, and audio tools. The console at `/admin/` manages channels, upstream accounts, users, tokens, logs, and settings.
- 💾 **Flexible storage** — SQLite out of the box; MySQL or PostgreSQL for shared storage; Redis for caching and synchronization; master/slave multi-node deployments.
- 🛠️ **Modern stack** — a Go backend with TypeScript and Vite frontends; every frontend build runs its type check first.

Provider and model support varies by channel implementation. Check the channel settings and test a model before relying on it in production.

## Using Studio

Configure a provider channel and enable the model for your group in the admin console. Sign in to Studio and select the model for the task. Playground and the media tools use your Studio session, and model calls draw from your account quota.

| Open | What you can do |
| --- | --- |
| **Chat → Playground** | Send native OpenAI Chat Completions, OpenAI Responses, Anthropic Messages, or Gemini requests. Inspect raw JSON or streaming events, compare two models with the same request, and copy a cURL example. |
| **Workspace → Knowledge libraries** | Upload private UTF-8 TXT or Markdown files, select a library in Chat, and open cited passages from replies. Keyword search runs against the existing database without embeddings or a vector store. Each library holds up to 64 documents and 2 MiB total, with a 256 KiB limit per document. |
| **Image generation → Image / Video** | Generate images with supported models, including Volcengine Ark Seedream and compatible Gemini models. Preview or download originals and attach images to Chat or Work. Seedream and compatible Gemini image models accept PNG, JPEG, and WebP references. Seedance on Ark generates 720p clips of 5–8 seconds from text with an optional first frame. Gemini `gemini-omni-1.2-flash` generates 360p text-to-video clips of 3–8 seconds. |
| **Audio** | Select a compatible model to generate speech, transcribe an audio file, or translate speech into English. Play and download generated audio or export the transcript. |
| **Account → Redeem** | Redeem a code or buy a one-time quota package through Stripe Checkout or NOWPayments when enabled. For crypto, select a currency and network enabled by the site operator. |

The operator sets Ark model rates with `ARK_MEDIA_PRICES` and defines fixed-price USD packages with `PAYMENT_PACKAGES`. To offer crypto payments, configure the gateway credentials and explicitly enable one or more options with `NOWPAYMENTS_PAY_CURRENCIES`. For example, `usdtsol,usdcmatic,btc` enables USDT on Solana, USDC on Polygon, and BTC on Bitcoin. The [media and payments guide](docs/media-and-payments.md) covers setup, gateway minimums, and the full list of supported combinations.

The Studio header lets you choose from ten interface languages, including right-to-left layouts for Arabic and Persian. The admin console has its own Chinese and English preference.

A single instance can use SQLite without running Redis or a vector database. Keep the database and the data volume when backing up or moving the deployment. Chat attachments and generated image and video files are stored on disk.

## Quick start with Docker

Build the image from the repository root:

```sh
git clone https://github.com/infinmalum/a1o-platform.git
cd a1o-platform
docker build -t a1o-platform:local .
docker run -d --name a1o-platform --restart unless-stopped -p 3000:3000 -v "$(pwd)/data:/data" a1o-platform:local
```

Open <http://localhost:3000/admin/setup> on first launch to create the root account. For unattended setup, pass `INITIAL_ROOT_PASSWORD` to the container before its first start. Once initialized, open Studio at <http://localhost:3000/> or the admin console at <http://localhost:3000/admin/>. Keep the data volume when replacing the container. For a public deployment, configure HTTPS and a persistent `SESSION_SECRET`.

The checked-in `docker-compose.yml` builds the image locally, tags it `a1o-platform:local`, and stores data under `a1o-platform`-named database and volume paths. If you deployed an earlier compose file that used the inherited `one-api` names, migrate your database and `data/` directory before switching.

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

Vite writes the two apps to `web/build/studio` and `web/build/admin`. The Go binary embeds both builds. Lapua drives the model and tool loop in both Chat and Work. A1O supplies the permitted tools and owns model routing, billing, persistence, and scheduling.

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

## Documentation

- [Frontend development](web/README.md)

## Development

- Run `npm run typecheck --prefix web/studio` or `npm run typecheck --prefix web/admin` for the app you are editing. Rebuild both apps before compiling a binary with the latest frontend assets.
- Run `go test ./...` for backend changes.

Report bugs and changes in [this repository's issues](https://github.com/infinmalum/a1o-platform/issues).

## Origin and licenses

A1O Platform is derived from [One API](https://github.com/songquanpeng/one-api), copyright © 2023 JustSong and other contributors. The upstream MIT license and notice are preserved in [`LICENSE.upstream`](LICENSE.upstream). Original changes made in A1O Platform are offered under the [Apache License 2.0](LICENSE); see [`NOTICE`](NOTICE) for attribution and scope. Existing per-file and third-party license notices remain applicable.

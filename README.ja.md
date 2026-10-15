<div align="center">
  <img src="docs/logo.png" alt="One Gateway ロゴ" width="120" />

  # One Gateway

  **OpenAI・Anthropic・Gemini の各プロトコルをネイティブに話す、すべてのモデルプロバイダー向けゲートウェイ。**

  [![CI](https://github.com/infinmalum/one-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/one-gateway/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
  ![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
  ![Node.js](https://img.shields.io/badge/Node.js-24-339933?logo=nodedotjs&logoColor=white)

  [English](README.md) · [简体中文](README.zh-CN.md) · 日本語
</div>

---

One Gateway は [One API](https://github.com/songquanpeng/one-api) から派生し、独自に保守されている AI ゲートウェイです。OpenAI（Chat Completions と Responses）・Anthropic Messages・Gemini generateContent の各 API 形式をネイティブにサポートし、ひとつのデプロイで複数のモデルプロバイダーを提供し、チャネル、ユーザー、トークン、利用枠、利用ログを Web 画面で管理できます。One API は JustSong 氏と他の貢献者によって開発されたものであり、One Gateway はその成果を基にしています。

> [!NOTE]
> このリポジトリは One API とは独立して保守されています。One API のリリース、Docker イメージ、デモサイト、Issue は One Gateway のリリースやサポート窓口ではありません。

## 主な特徴

- 🔌 **4 つのネイティブ API 形式** — OpenAI Chat Completions、OpenAI Responses（照会・キャンセル可能なバックグラウンド実行を含む）、Anthropic Messages、Gemini `generateContent`。同じトークンで embeddings・画像・音声・モデレーションも利用可能。
- 🌉 **プロトコル間ブリッジ** — クライアントと上流の形式が異なっても、リクエストとレスポンスを相互変換します（ストリーミング対応）。OpenAI クライアントは Anthropic・Gemini・Responses の上流へ、Anthropic クライアントは OpenAI chat・Gemini の上流へ接続できます。
- 🔀 **スマートルーティング** — 37 種類のプロバイダーアダプター、チャネルの負荷分散、モデル名マッピング、グループ別倍率、OAuth で接続したサブスクリプションアカウント（ChatGPT プラン）、サブスクリプション優先のルーティングポリシー、最初のバイトがクライアントに届く前の自動リトライ。
- 🤖 **内蔵チャットランタイム** — サーバー側のエージェントツールループ。永続化されたセッション、サンドボックス化した Python / Node コード実行（rlimits による資源制限とネットワーク分離）、MCP（Model Context Protocol）ツールサーバーをサポート。
- 🧮 **利用枠と課金** — トークンごとの利用枠、利用ログ、引換コード、チャージ、すべてのリクエストを倍率に基づいて正確に精算。
- 🖥️ **Web コンソール** — ダッシュボード、チャネル、アップストリームアカウント、ユーザー、トークン、ログ、設定を一画面で管理。内蔵チャットページと画像・デシジョンプレイグラウンド付き。
- 💾 **柔軟なストレージ** — 初期状態で SQLite、共有ストレージに MySQL / PostgreSQL、キャッシュと同期には Redis を利用可能。master/slave のマルチノード構成にも対応。
- 🛠️ **モダンな技術スタック** — Go バックエンドと TypeScript + Vite フロントエンド。フロントエンドのビルド前には必ず型チェックを実行。

対応モデルと機能はチャネルの実装によって異なります。本番運用前に設定画面で確認し、対象モデルをテストしてください。

## Docker ですぐに起動する

リポジトリのルートでイメージをビルドします。

```sh
git clone https://github.com/infinmalum/one-gateway.git
cd one-gateway
docker build -t one-gateway:local .
docker run -d --name one-gateway --restart unless-stopped -p 3000:3000 -v "$(pwd)/data:/data" one-gateway:local
```

<http://localhost:3000> を開いてください。新しいデータベースでは、起動ログに一度だけ出力されるランダムなパスワードで `root` アカウントが作成されます（`INITIAL_ROOT_PASSWORD` で事前指定も可能です）。**初回ログイン後、直ちにパスワードを変更してください。**マウントしたディレクトリには標準の SQLite データベースが保存されるため、コンテナを更新しても保持してください。外部公開する場合は HTTPS と固定の `SESSION_SECRET` も設定してください。

同梱の `docker-compose.yml` はイメージをローカルでビルドし、`one-gateway:local` タグを付けます。データベース名とデータの保存先は `one-gateway` 系の名前に統一されています。以前の `one-api` 系の名前を使う compose ファイルで運用している場合は、切り替え前にデータベースと `data/` ディレクトリを移行してください。

## ソースからビルドする

Dockerfile は Node.js 24 と Go 1.27.1 を使用します。対応する環境でフロントエンドを先にビルドし、`web/build` を埋め込む Go バイナリを作成します。

```sh
npm ci --legacy-peer-deps --prefix web/default
npm run build --prefix web/default
go build -o one-gateway .
./one-gateway --port 3000
```

ダウンロードした npm と Go のモジュールは、それぞれ標準の永続キャッシュを使用します。フロントエンドの依存関係は `web/default/node_modules` に入り、成果物は `web/build/default` に出力されます。

## 設定と使い方

起動前に環境変数を設定できます。主な設定は次のとおりです。

| 環境変数 | 用途 |
| --- | --- |
| `PORT` | HTTP ポート。既定値は `3000`。 |
| `SQL_DSN` | MySQL DSN または `postgres://` URL。未設定なら SQLite。 |
| `SQLITE_PATH` | SQLite 使用時のデータベースファイルのパス。 |
| `SESSION_SECRET` | 固定のセッションシークレット。再起動やマルチノード構成では特に重要。 |
| `REDIS_CONN_STRING` | キャッシュ用の Redis 接続 URL。 |
| `SYNC_FREQUENCY` | キャッシュ同期の間隔（秒）。 |
| `NODE_TYPE` | マルチノード構成での `master` または `slave`。 |
| `FRONTEND_BASE_URL` | スレーブノードのフロントエンドリダイレクト先（任意）。 |

マルチノード構成では、すべてのノードを同じ MySQL / PostgreSQL データベースに接続し、同じ `SESSION_SECRET` を共有して、ノードの役割とキャッシュ同期を設定してください。設定の正確な一覧は [`common/config`](common/config) のコードを参照してください。

ログイン後、**チャネル** ページでプロバイダーのキーを登録し、**トークン** ページでアクセストークンを作成します。クライアントはそれぞれの形式で対応するエンドポイントにアクセスし、同じトークンがすべてのエンドポイントで使えます:

| クライアント形式 | エンドポイント |
| --- | --- |
| OpenAI Chat Completions | `POST /v1/chat/completions` |
| OpenAI Responses | `POST /v1/responses`（バックグラウンド実行: `GET /v1/responses/{id}`、`POST /v1/responses/{id}/cancel`） |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `POST /v1beta/models/{モデル名}:generateContent` または `:streamGenerateContent`（`/v1` でも利用可） |
| OpenAI Embeddings | `POST /v1/embeddings` |
| OpenAI 画像 / 音声 / モデレーション | `POST /v1/images/generations`、`/v1/audio/speech`、`/v1/audio/transcriptions`、`/v1/audio/translations`、`/v1/moderations` |
| SystemOne 評価 | `POST /v1/systemone` |

上流チャネルがクライアントと同じ形式である必要はありません。ゲートウェイがプロトコル間をブリッジするため、Anthropic 形式のクライアントを OpenAI chat や Gemini チャネルで処理でき、OpenAI Chat リクエストは Responses ブリッジ経由で ChatGPT サブスクリプションアカウントでも実行できます。

OpenAI 互換クライアントのエンドポイントを `http://localhost:3000/v1` に設定し、作成したトークンを API キーとして指定してください：

```sh
curl http://localhost:3000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello!"}]}'
```

## ドキュメント

- [フロントエンドテーマの説明](web/README.md)
- [TypeScript 移行メモ](docs/typescript-migration.md)

## 開発

- フロントエンド編集中は `npm run typecheck --prefix web/default` を実行できます。
- 埋め込みアセットを変更する前に、`npm run build --prefix web/default` を実行してください。
- バックエンドの変更には `go test ./...` を使用します。

不具合や変更は[このリポジトリの Issue](https://github.com/infinmalum/one-gateway/issues) で報告してください。

## 由来とライセンス

One Gateway は [One API](https://github.com/songquanpeng/one-api) から派生したプロジェクトで、原著作者は © 2023 JustSong および他の貢献者です。元プロジェクトの MIT ライセンスと表示は [`LICENSE.upstream`](LICENSE.upstream) に保存されています。One Gateway の独自の変更は [Apache License 2.0](LICENSE) の下で提供され、帰属と適用範囲は [`NOTICE`](NOTICE) を参照してください。各ソースファイルおよびサードパーティ依存関係の元のライセンス表示は引き続き適用されます。

<div align="center">
  <img src="docs/logo.png" alt="A1O Platform ロゴ" width="120" />

  # A1O Platform

  **OpenAI・Anthropic・Gemini の各プロトコルをネイティブに話す、すべてのモデルプロバイダー向けの単一プラットフォーム。**

  [![CI](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-AGPL_v3.0-blue.svg)](LICENSE)
  ![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
  ![Node.js](https://img.shields.io/badge/Node.js-24-339933?logo=nodedotjs&logoColor=white)

  [English](README.md) · [简体中文](README.zh-CN.md) · 日本語 · [Français](README.fr.md) · [Русский](README.ru.md) · [Español](README.es.md)
</div>

---

A1O Platform は [One API](https://github.com/songquanpeng/one-api) から派生し、独自に保守されている AI ゲートウェイです。OpenAI（Chat Completions と Responses）・Anthropic Messages・Gemini generateContent の各 API 形式をネイティブにサポートし、ひとつのデプロイで複数のモデルプロバイダーを提供し、チャネル、ユーザー、トークン、利用枠、利用ログを Web 画面で管理できます。One API は JustSong 氏と他の貢献者によって開発されたものであり、A1O Platform はその成果を基にしています。

> [!NOTE]
> このリポジトリは One API とは独立して保守されています。One API のリリース、Docker イメージ、デモサイト、Issue は A1O Platform のリリースやサポート窓口ではありません。

## 主な特徴

- 🔌 **4 つのネイティブ API 形式** — OpenAI Chat Completions、OpenAI Responses（照会・キャンセル可能なバックグラウンド実行を含む）、Anthropic Messages、Gemini `generateContent`。同じトークンで embeddings・画像・音声・モデレーションも利用可能。
- 🌉 **プロトコル間ブリッジ** — クライアントと上流の形式が異なっても、リクエストとレスポンスを相互変換します（ストリーミング対応）。OpenAI クライアントは Anthropic・Gemini・Responses の上流へ、Anthropic クライアントは OpenAI chat・Gemini の上流へ接続できます。
- 🔀 **スマートルーティング** — チャネルの負荷分散、モデル名マッピング、グループ別倍率、OAuth で接続したサブスクリプションアカウント（ChatGPT プラン、Claude Code、Gemini CLI）、サブスクリプション優先のルーティングポリシー、最初のバイトがクライアントに届く前の自動リトライ。
- 🤖 **内蔵チャットランタイム** — Chat と Work で [Norma](https://github.com/infinmalum/norma-e) のツールループを共有。永続化されたセッション、サンドボックス化した Python / Node コード実行（rlimits による資源制限とネットワーク分離）、MCP（Model Context Protocol）ツールサーバーをサポート。
- 🧮 **利用枠と課金** — トークンごとの利用枠、利用ログ、引換コード、チャージ、すべてのリクエストを倍率に基づいて正確に精算。
- 🖥️ **Web アプリ** — `/` の Studio でマルチプロトコル Playground 付きチャット、バックグラウンドタスク、エージェント、非公開の資料ライブラリ、画像・動画・音声ツールを利用できます。`/admin/` の管理コンソールではチャネル、上流アカウント、ユーザー、トークン、ログ、設定を管理します。
- 💾 **柔軟なストレージ** — Docker Compose の既定は PostgreSQL。SQLite と MySQL にも対応、キャッシュと同期には Redis を利用可能。master/slave のマルチノード構成にも対応。
- 🛠️ **モダンな技術スタック** — Go バックエンドと TypeScript + Vite フロントエンド。フロントエンドのビルド前には必ず型チェックを実行。

対応モデルと機能はチャネルの実装によって異なります。本番運用前に設定画面で確認し、対象モデルをテストしてください。

## Studio の使い方

管理コンソールでプロバイダーのチャネルを設定し、対象グループでモデルを有効にします。Studio にログインして用途に合ったモデルを選択します。Playground とメディアツールは Studio のログインセッションを使用し、モデルの利用分はアカウントの利用枠から差し引かれます。

| 画面 | できること |
| --- | --- |
| **Chat → Playground** | OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini のネイティブ形式でリクエストを送信できます。JSON やストリーミングイベントの確認、同じリクエストでの 2 モデルの比較、cURL サンプルのコピーが可能です。 |
| **Workspace → Knowledge libraries** | 非公開の UTF-8 TXT や Markdown をアップロードし、Chat でライブラリを選択して、回答に引用された箇所を開けます。既存のデータベースでキーワード検索を行うため、埋め込みやベクトルデータベースは不要です。1 ファイルあたり 256 KiB、1 ライブラリあたり 64 ファイルかつ合計 2 MiB までです。 |
| **画像生成 → 画像／動画** | Volcengine Ark Seedream や対応する Gemini モデルなどで画像を生成し、プレビュー、原本のダウンロード、Chat や Work への添付ができます。Seedream と対応する Gemini 画像モデルでは PNG、JPEG、WebP の参照画像を指定できます。Ark の Seedance ではテキストから 720p、5～8 秒の動画を生成でき、先頭フレームも指定できます。Gemini の `gemini-omni-1.2-flash` はテキストから 360p、3～8 秒の動画を生成します。 |
| **音声** | 対応モデルで読み上げ音声の生成、文字起こし、音声から英語への翻訳が可能です。生成音声の再生とダウンロード、文字起こしのテキスト出力に対応します。 |
| **Account → Redeem** | 引換コードを使うか、サイトで有効になっている Stripe Checkout、Airwallex、EPay V1/V2、NOWPayments、GMPay Edge で買い切りの利用枠を購入できます。 |

運営者は `ARK_MEDIA_PRICES` で Ark モデルの利用単価を設定し、`PAYMENT_PACKAGES` でパッケージ価格を定義します。Stripe、Airwallex、NOWPayments、GMPay には米ドルのセントを使い、EPay には別途、中国人民元の分で価格を設定します。NOWPayments の通貨とネットワークは `NOWPAYMENTS_PAY_CURRENCIES` で有効にし、GMPay の受取方法は GMPay Edge で設定します。設定とテストの手順は[メディアと決済の運用ガイド](docs/media-and-payments.md)を参照してください。

Studio のヘッダーで 10 種類の表示言語を選択できます。アラビア語とペルシア語では右から左に表示します。管理コンソールでは中国語または英語を別に設定できます。

単一インスタンスでは SQLite を利用でき、Redis やベクトルデータベースは不要です。バックアップや移行ではデータベースとデータボリュームを保持してください。チャットの添付や生成した画像・動画はディスクに保存されます。

## Docker ですぐに起動する

新規セットアップは、Docker Compose と OpenSSL を利用できる環境で実行します。

```sh
git clone https://github.com/infinmalum/a1o-platform.git
cd a1o-platform
umask 077
cp .env.example .env
printf 'POSTGRES_PASSWORD=%s\nSESSION_SECRET=%s\n' "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" >> .env
docker compose up -d
```

初回起動後に <http://localhost:3000/admin/setup> を開き、root アカウントを作成します。無人セットアップでは、コンテナの初回起動前に `INITIAL_ROOT_PASSWORD` を渡します。初期化後は <http://localhost:3000/> で Studio、<http://localhost:3000/admin/> で管理コンソールを開けます。コンテナを交換するときはデータボリュームを維持してください。公開環境では HTTPS と固定の `SESSION_SECRET` を設定します。

既定の Compose デプロイは PostgreSQL を起動し、データを `./data/postgres` に保存します。SQLite または既存の PostgreSQL / MySQL を利用するには `docker compose -f docker-compose.external.yml up -d` を使用し、外部データベースには `SQL_DSN` を設定します。Redis は任意です。

以前の同梱 MySQL を使う環境を更新する場合は、既存のプロジェクトディレクトリと認証情報を保持し、`docker compose -f docker-compose.mysql.yml up -d` を実行します。データベースを切り替える前に、既存データのバックアップ、移行、検証を行います。手順と切り戻しは[データベース運用ガイド](docs/database-deployment.md)を参照してください。

## ソースからビルドする

Node.js 24、Go 1.27.1、および CGO SQLite ドライバーに必要な C コンパイラを用意し、リポジトリのルートから実行します。

```sh
npm ci --legacy-peer-deps --prefix web/studio
npm run build --prefix web/studio
npm ci --legacy-peer-deps --prefix web/admin
npm run build --prefix web/admin
go build -o a1o-platform .
./a1o-platform --port 3000
```

Vite は `web/build/studio` と `web/build/admin` に成果物を出力し、Go バイナリは両方を埋め込みます。Norma は Chat と Work のモデルとツールの実行ループを担当します。A1O は許可済みのツールを提供し、モデルのルーティング、課金、永続化、スケジューリングを管理します。

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

- [フロントエンド開発](web/README.md)
- [アカウントのプライバシー、長期メモリ、データの削除](docs/privacy.md)

## 開発

- 編集対象に応じて `npm run typecheck --prefix web/studio` または `npm run typecheck --prefix web/admin` を実行します。最新のフロントエンドを埋め込むには、Go バイナリをビルドする前に両方のアプリをビルドします。
- バックエンドの変更には `go test ./...` を使用します。

不具合や変更は[このリポジトリの Issue](https://github.com/infinmalum/a1o-platform/issues) で報告してください。

## 由来とライセンス

A1O Platform は [One API](https://github.com/songquanpeng/one-api) から派生したプロジェクトで、原著作者は © 2023 JustSong および他の貢献者です。元プロジェクトの MIT ライセンスと表示は [`LICENSE.upstream`](LICENSE.upstream) に保存されています。A1O Platform の独自の変更は [GNU Affero General Public License v3.0（AGPL-3.0）](LICENSE) の下で提供され、帰属と適用範囲は [`NOTICE`](NOTICE) を参照してください。各ソースファイルおよびサードパーティ依存関係の元のライセンス表示は引き続き適用されます。

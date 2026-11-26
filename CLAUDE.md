# CLAUDE.md

このリポジトリで作業する際の案内。

## プロジェクト概要

launchs-org 用に、Kubernetes 上でのデプロイ管理を行うバックエンド/フロントエンドを作成するプロジェクト。詳細は [readme.md](readme.md) を参照。

- バックエンド: Golang + Echo + Gorm(PostgreSQL)
- フロントエンド: Next.js + React + TypeScript(`frontend/`。[dev-docs/frontend.md](dev-docs/frontend.md))
- インフラ: 開発環境 Podman(Docker)、本番環境 Kubernetes(オンプレ)

## ディレクトリ構成

```
backend/        バックエンド(Golang/Echo)。dockerfile と src/(コード)を持つ
watcher/        k8s の Pod と Deployment を観測し、実際の状態を DB に反映するコンテナ(dev-docs/watcher.md)
admin/          管理コンテナ(管理者だけの API `/admin-api`。画面はフロントエンドの `/admin`。[dev-docs/endpoints/admin.md](dev-docs/endpoints/admin.md))
auth/           認証(ユーザー登録・ログイン・トークンの発行・JWKS。dev-docs/auth.md)
frontend/       フロントエンド(React + Vite + TypeScript)。dev-docs/frontend.md
logcollector/   deploy のコンテナの出力ログを Pod ごとに購読して DB に保存するコンテナ(dev-docs/logs.md)
accessstats/    ingress のアクセスの集計を、Traefik のログから DB に保存するコンテナ(dev-docs/ingress-plugins.md)
traefik-plugins/ Traefik のプラグイン(launchs-edge、launchs-stats)と、Yaegi での試験(harness)
accessstats/    Traefik のログから、ingress のアクセスの集計を読み取って DB に保存するコンテナ(dev-docs/ingress-plugins.md)
traefik-plugins/ Traefik のプラグイン(launchs-edge、launchs-stats)と、Yaegi での試験(harness)
exporter/       アクセスログを、利用者が設定した送り先(webhook と Google Drive)に書き出すコンテナ(dev-docs/log-export.md)
docs/           利用者向けのドキュメント(Markdown の原稿。`tools/docsbuild` が、静的なサイトにして、GitHub Pages に公開する。画面の言葉に合わせて書く。画面や仕様を変えたら、ここも直す。`api.md` は API と自動デプロイの案内で、**エンドポイントを足したら、一覧も直す**。書き方は [dev-docs/user-docs.md](dev-docs/user-docs.md))
tools/docsbuild/ 利用者向けのドキュメントを、HTML にする生成ツール(Go。`task docs:build`、`task docs:test`)
deploy/         本番向けの Kubernetes のマニフェスト(kustomize + Argo CD。Argo CD の Application で全部入る。設定は deploy/argocd.md。deploy/README.md)
registry/       同じ形のコンテナ用フォルダ(これから作る)
templates/      テンプレート(別リポジトリ launchs-v2-template の git submodule。manifest.yaml + README.md + 版のフォルダ)
shared/         バックエンド・ワーカー・watcher で共有するコード
migrations/     DB のマイグレーション(up のみ)
dev-docs/       開発者向けのドキュメント(設計、仕様、規約、作業の記録)。利用者向けは docs/(別物)
  endpoints/    確定したエンドポイント仕様(実装のソースオブトゥルース)
  issues/       エンドポイントごとの詳細設計レビュー用Issue
  talks/        作業の経緯・設計判断の記録(会話ログ)
  pre-implementation-checklist.md  実装着手前に決める確認項目(回答欄付き)
  coding-conventions.md  コードの書き方とファイルのルール
  call-flow.md  コードの呼び出しの流れ(どこで何を組み立てるか、リクエストの流れ)
docker-compose.yaml  開発環境用(設定の例は .env.example。コピーして .env にする)
Taskfile.yml    開発のコマンド(go-task。全てコンテナの中で実行する)
```

## 実装方針(確定分)

実装前に決めた技術方針は [dev-docs/pre-implementation-checklist.md](dev-docs/pre-implementation-checklist.md) に回答と決定を残している。実装時はまずそちらを確認すること。主な点は以下。

- 構成: 認証・バックエンド(API)・ワーカー(Temporal Worker)・レジストリ(自作)・ログ収集は別コンテナ。プロジェクトルート直下にそれぞれフォルダを分け、フォルダの直下に `dockerfile`、その下の `src/` にコード(`src/main.go` など)を置く。
- バックエンドとワーカーで共有するコード(DBのモデル、Workflow/Activity の名前と入出力の型)は `shared/` に置く(`src/` は挟まない)。
- Go モジュールはルートに `go.mod` を1つ置き、モジュール名は `launchs`。docker のビルドコンテキストはリポジトリルートにする(コンテナごとに Dockerfile で使うフォルダを選ぶ)。
- ビルド・テスト・実行は、全てコンテナの中で行う(ホストの Go は `go.mod` より古い)。開発のコマンドは `Taskfile.yml`(go-task)にまとめる。
- コードを書くときのルールは [dev-docs/coding-conventions.md](dev-docs/coding-conventions.md) に従う。特に次を守る。
  - **一文字の変数名は禁止**(ループの添字、レシーバーも含む)。
  - **ファイル名は役割が分かる名前にする**(`project_controller.go` のように層の名前を付ける)。
  - **全ての関数・メソッドに、詳細な日本語のコメントを書く**(処理の流れ、引数、戻り値、エラー、副作用)。
  - **関数の中は、行ごとにコメントを書く**(1つの文ごとに、直前の行へ日本語で。テストも同じ)。
  - マイグレーションは up だけ書く。
- Git の運用: `develop` から `feature/<短い説明>` のブランチを切り、細かい単位で作業し、プルリクエストで `develop` に入れる。マージは本人が行う。Claude は、依頼がない限り push やプルリクエストの作成をせず、マージもしない。
- コンテナ名は `backend`(または `app`)、`worker` など。コンテナの名前と、コンテナ内部のコード構成は別の話として扱う。
- コンテナ内部のコード構成は `controller / service / repository / model` の層でファイルを分ける。
- **k8s の実際の状態(Pod、Deployment のレプリカ数など)を DB に書くのは、watcher だけ**(詳細は [dev-docs/watcher.md](dev-docs/watcher.md))。watcher は同時に1つだけ動かし、リーダー選出は PostgreSQL の advisory lock(`shared/leader`)で行う。
- **プロジェクトの namespace は、NetworkPolicy で隔離する**(namespace の作成時に適用。詳細は [dev-docs/network-isolation.md](dev-docs/network-isolation.md))。外からの通信を中継する namespace(Traefik など)は、環境変数 `INGRESS_NAMESPACE` で許可する。
- **ingress は、Traefik の IngressRoute として作る**(プロジェクトに属する。ホスト名は「形容詞-色-動物.ベースドメイン」で自動で払い出す。詳細は [dev-docs/ingress.md](dev-docs/ingress.md))。ベースドメインと払い出さない名前は、環境変数 `INGRESS_BASE_DOMAIN`、`INGRESS_RESERVED_SUBDOMAINS`。
- **volume は、k8s の PVC として作る**(プロジェクトに属する。PVC は、最初にマウントを適用するときに作る。詳細は [dev-docs/volume.md](dev-docs/volume.md))。大きさの上限、StorageClass、アクセスモードは、環境変数 `VOLUME_MAX_SIZE_MB`、`VOLUME_STORAGE_CLASS`、`VOLUME_ACCESS_MODE`。**deploy の Pod の requests は、limits の何 % にするかを、環境変数 `CPU_REQUEST_PERCENT`、`MEMORY_REQUEST_PERCENT`(1 から 100。既定 100=同じ)で決める**(CPU を小さくすると、収容できる数が増える。deploy/questions.md)。
- **状態による操作の可否と、状態を変える操作は、`shared/model` のメソッドに置く**(repository は、ロックして読む → メソッドを呼ぶ → 保存する、だけを行う。[dev-docs/coding-conventions.md](dev-docs/coding-conventions.md))。
- **認証は、別のコンテナ(`auth/`)**。URL は `/auth/...`(パスを書き換えずに渡す)。access token は EdDSA で、backend が JWKS の公開鍵で検証する(`shared/jwtverify`。環境変数 `AUTH_JWKS_URL`。本番は `AUTH_REQUIRE_JWKS=true` で、開発用の `DEV_USER_ID` を拒否する)。アカウント管理(管理者・BAN・招待)は `/auth/admin/...`(管理者かどうかは、毎回 DB で確かめる。最初の管理者は `task admin:create -- <userName>`。[dev-docs/endpoints/auth_admin.md](dev-docs/endpoints/auth_admin.md))。クォータ(アカウントごとの上限)は、アプリの DB の `quota_defaults` / `account_quotas`。作成・拡張のトランザクションの中で、ユーザーの行をロックして判定し、超えたら 409([dev-docs/auth.md](dev-docs/auth.md) §8・§13、[dev-docs/endpoints/quota.md](dev-docs/endpoints/quota.md))。
- **管理画面(`/admin`)と管理コンテナ(`admin/`)**: 管理者(bootstrap のアカウントなど。`role` が admin)だけが入れる別の画面(`components/admin/`。概要、ユーザー、招待、ログイン履歴、デプロイ、アクセスログ、クォータ、設定。管理者でないと、入れない)。アカウント系の API は auth の `/auth/admin/...`(ユーザー、招待、**ログイン履歴 `login-events`**)、それ以外(deploy、クォータ、アクセスログ、セキュリティ)は、別コンテナ `admin/` の `/admin-api/...`(access token の role が admin のものだけを通す。`overview`、`deploys`、`access-logs`、`quota-defaults`(全体のクォータ。PUT で変更)、`settings`(読むだけ。変更はデプロイの設定で)。ユーザーごとのクォータは backend の `/api/v1/admin/users/:id/quota`。`security/…`(不審な IP、IP のブロック=auth のログイン・サインアップを 403、管理操作の監査ログ。設計と限界は [dev-docs/security.md](dev-docs/security.md)。ブロックと監査の表は auth の DB で、admin は `AUTH_DATABASE_URL` でも接続する))。
- **招待の画面**: アカウント設定の「招待」(`components/invitation-section.tsx`。**管理者だけに出る**。招待コード(1 回だけ有効)の発行、一覧、取り消し。API は `/auth/admin/invitations`、[dev-docs/endpoints/auth_admin.md](dev-docs/endpoints/auth_admin.md))。
- **ConfigMap は、プロジェクトの資源として共有できる設定ファイルの組**(テキストと base64 のバイナリ。保存する大きさは 1 MiB まで。編集は新しい版を足すだけで、deploy は版を固定してマウントする)。段階 1(資源の API、版)、段階 2(deploy へのマウント。`/deploy/:id/configmap-mounts`、worker の Workflow)、段階 3(画面。上のバーの「ConfigMap」タブと、deploy パネルの「ConfigMap」タブ)は、実装済み。設計は [dev-docs/configmap.md](dev-docs/configmap.md)、仕様は [dev-docs/endpoints/configmaps.md](dev-docs/endpoints/configmaps.md)。
- **テンプレート(よく使う構成を、変数を埋めるだけで、まとめて作る)**: **別リポジトリ [launchs-v2-template](https://github.com/launchs-org/launchs-v2-template) で管理し、本体には、git submodule として `templates/` に取り込む**(CI の checkout は `submodules: true`。**`--recursive` にしない**: テンプレート側の `harness/launchs` が、この本体を指すため。非公開なので、読み取りの token を secret `TEMPLATES_TOKEN` に入れる)。1 つにつき 1 つのフォルダ: `templates/<id>/manifest.yaml`(名前、説明、アイコン、分類、既定の版)、`README.md`、**版のフォルダ `<x.y.z>/`**(`config.yaml`=変数と notes、`volumes.yaml`、`configmaps.yaml`、`deploy.yaml`、`service.yaml`、`ingress.yaml`、`files/`)。**利用者は、作るときに版を選べる**(`GET /templates/:id?version=`、apply の `version`。使った版は `template_applies.template_version` に残る)。エンジン(読み込み、検証、展開、計画)は `shared/template`、計画の型は `shared/templateplan`。deploy・volume・ConfigMap(共有)・service・ingress を、1 つのテンプレートで作れる。`POST /projects/:id/templates/:tid/apply` が、変数の検査とクォータの確認をして、計画を保存し、Workflow(`ApplyTemplateWorkflow`)が、既存の Workflow を子として、順に作る(失敗したら止まり、作れたものは残す)。deploy は、最初から、マウントとファイルを持った形で作る(起動前に、PVC と ConfigMap を用意する)。同梱は 17 個(hello-world(自動デプロイの入口。仮のイメージ traefik/whoami)、nginx、PostgreSQL、MariaDB、Redis、code-server、WordPress、Uptime Kuma、Grafana、Excalidraw、Vaultwarden、Memos、n8n、Gitea、CloudBeaver、Redash、Mailpit)。テンプレートを選んだあとの入力の画面に、**README.md が「使い方と説明」として出る**(`GET /templates/:id` の `readme`。Markdown の表示は `components/markdown-view.tsx`。使い方は、README に書く)。作成後の案内(`notes`)と、非 root のイメージ用の `fsGroup`(deploy の項目。利用者は変えられない)がある。**テンプレートの開発と検証(雛形 `task new`、本物のエンジンと展開コードで試験する harness、docker の中の k3s)は、そのリポジトリの README と docs**。設計は [dev-docs/templates.md](dev-docs/templates.md)、仕様は [dev-docs/endpoints/templates.md](dev-docs/endpoints/templates.md)、画面は [dev-docs/frontend.md](dev-docs/frontend.md)。
- **コンテナの出力ログは、別コンテナ `logcollector/` が Pod のログを購読して DB に保存する**(書き手は logcollector だけ。リーダー選出は advisory lock。設計と実装メモは [dev-docs/logs.md](dev-docs/logs.md)。収集と API(`GET /deploy/:deployId/logs`、deploy の詳細の `logPods`)まで実装済み。Talos の実機で確認済みだが、大量出力時の取り込みの停止(原因は未特定)などの注意点が、§14 にある)。
- **ingress の本当のクライアント IP と、アクセスの集計は、Traefik のプラグイン(`traefik-plugins/`。設計は [dev-docs/ingress-plugins.md](dev-docs/ingress-plugins.md))**。`launchs-edge`(Traefik 全体に 1 つ。Cloudflare のヘッダーから `X-Real-Ip` と `X-Forwarded-Proto/Port` を直す。信頼する範囲は `trustedCIDRs`)と、`launchs-stats`(ingress ごとの Middleware。1 分ごとに 1 行 `LAUNCHS_STATS {json}` を標準出力へ)。worker の `INGRESS_STATS_PLUGIN` が空なら機能は無効(既定)。集計は別コンテナ `accessstats/` が Traefik のログから DB(`ingress_access_minutes` / `ingress_access_hours`)へ保存し、`GET .../access-stats` で返す。ingress の `accessStats`(既定 true)は `PATCH` で切り替える。プラグインは `traefik-plugins/harness` で Yaegi 上でも試験する(`cd traefik-plugins/<dir> && go test ./...` を、コンテナの中で)。
- **本当のクライアント IP と、ingress ごとのアクセスの集計は、Traefik のプラグイン(Yaegi。`traefik-plugins/`)**。`launchs-edge`(entryPoint 全体。Cloudflare のヘッダーを、信頼する接続元(`trustedCIDRs`)からだけ信じて、`X-Real-Ip` と `X-Forwarded-Proto/Port` を直す。code-server の WebSocket 1006 の対策)と `launchs-stats`(ingress ごとの Middleware。1 分ごとに、件数、ステータス、メソッド、パス、IP、応答時間を、標準出力に 1 行の `LAUNCHS_STATS {json}` で出す)。worker の環境変数 `INGRESS_STATS_PLUGIN`(**空なら機能は無効。プラグインを Traefik に入れてから指定する**)で、反映のたびに Middleware を作る。別コンテナ `accessstats/` が、Traefik の Pod のログから読み取って DB(`ingress_access_minutes` / `ingress_access_hours`)に保存する(リーダーは advisory lock)。API は `GET /projects/:projectId/ingresses/:ingressId/access-stats?range=`、切り替えは `PATCH .../ingresses/:ingressId {accessStats}`(既定は有効)。1 リクエストごとのアクセスログ(URL はクエリも含めてそのまま保存。`ingress_access_logs`。既定 24 時間・ingress ごとに 10000 行)は `GET .../access-logs`。画面は ingress パネルの「アクセスログ」(表だけ。日時・パス・IP・ステータス・メソッドで絞り込み、更新ボタン)。プラグインの試験は `traefik-plugins/harness`(本物の Yaegi。`cd traefik-plugins/harness && go test ./...`)。**本番の Traefik には、プラグインを適用済み(2026-11-18。`launchs-edge` が効いている)。worker の `INGRESS_STATS_PLUGIN` と accessstats の本番での起動は、まだ**(手順は [dev-docs/ingress-plugins.md](dev-docs/ingress-plugins.md) §8、§12)。設計と実機確認の結果は同じファイル。
- **アクセスログの、外への書き出し(webhook と Google Drive)**: 転送の設定は ingress に属し、**アカウントごとに 10 個まで**(`export_destinations`)。Google Drive の接続は**アカウントに属する**(`google_drive_connections`。アカウント設定でつなぐ。権限は `drive.file` だけ=launchs が作ったファイルだけ。合鍵は AES-256-GCM で暗号化して保存)。送るのは、専用コンテナ **`exporter/`**(watcher と同じ仕組み。advisory lock の鍵 7100004。10 秒ごとに見回る)。webhook は、まとめ送り(既定 30 秒。10 / 30 / 60 / 300 秒から選ぶ。署名つき。SSRF 対策は `shared/safehttp`)。Drive は、**日本時間の日ごとのファイル**を、プロジェクトごとのフォルダ(`launchs-access-logs/<プロジェクト名>/<ホスト名>_<日付>.ndjson`)に、**1 時間ごと**(送り先ごとに、12 の枠で散らす。6000 行たまったら前倒し)。配送は「少なくとも 1 回」。**暗号化の鍵 `EXPORT_CREDENTIAL_KEY` が空なら、機能全体が無効**。Google の OAuth(`GOOGLE_OAUTH_CLIENT_ID`、`GOOGLE_OAUTH_CLIENT_SECRET`、戻り先 `GOOGLE_OAUTH_REDIRECT_URL` か `LAUNCHS_HOST` から作る)が無ければ、Drive だけが無効。アクセスログの保存は、既定 24 時間・ingress ごとに **10000 行**。設計は [dev-docs/log-export.md](dev-docs/log-export.md)、仕様は [dev-docs/endpoints/log_exports.md](dev-docs/endpoints/log_exports.md)、利用者向けは `docs/log-export.md`。**本番への適用(Secret の追加と、exporter の起動)は、まだ**。
- **ログインとサインアップの、人間であることの確認は、Cloudflare Turnstile**(`.env` の `TURNSTILE_SITE_KEY` と `TURNSTILE_SECRET_KEY` を、両方書いたときだけ有効。既定は無効。本番の frontend には `CSP_TURNSTILE=https://challenges.cloudflare.com` も要る。[dev-docs/auth.md](dev-docs/auth.md) §16)。
- **フロントエンドは、`frontend/`(Next.js + React + TypeScript。v0 で作った画面を土台にしている)**。同じホストの `/` に置き、`/api/` と `/auth/` を、backend と auth へ振り分ける(CORS は要らない)。開発は `next dev`(ポート 3000、nginx が `/` を向ける)、本番は standalone 出力を Node で動かす。`NEXT_PUBLIC_LAUNCHS_API_MODE=live` のとき本物の API、未指定だと、見せかけのデータ(`lib/mock-api.ts`)で動く(compose と dockerfile は live にしてある)。開発サーバーに別ホスト名(例: 172.30.1.1)でつなぐときは、`DEV_ALLOWED_ORIGINS`(compose が `HTTPS_BIND_ADDRESS` から作る)。型の検査は `task frontend:test`、本番イメージは `task frontend:build`。**以前の Vite 版にあった、テスト(vitest)と、厳しい CSP は、まだ無い**。 YAML の表示には Monaco Editor(`components/yaml-viewer.tsx`。**CDN は使わず**、`scripts/copy-monaco.mjs` が `public/monaco/vs`(gitignore)に写して、自分のサーバーから配る。`pnpm dev`・`build` の前と、開発コンテナの起動時に実行する。`monaco-editor` は、`@monaco-editor/react` が使える 0.52.2 に固定)。
- 責務分担: controller が pending の保存や即時反映系の書き込みを行い、非同期の apply は Temporal の Workflow/Activity が実行して結果確定後に正式値と status を更新する。

- **タグとメトリクス**: deploy・volume・service・ingress に、タグ(10 個まで。各テーブルの `tags`)を付けられ、`DELETE /projects/:projectId/tags/:tag/deploys` で、タグ単位に一斉に削除を受け付ける(ingress → service → deploy の順。volume は、データを守るため、対象にしない。各資源の削除と同じ規則。失敗した分は結果に出す)。テンプレートの資源には、`template:<id>` と `apply:<適用の識別>` が自動で付く。メトリクスは、watcher(`watcher/src/metrics`)が 30 秒ごとに Pod を観測し(CPU とメモリは metrics-server から。無ければ null)、`deploy_metric_samples` に 24 時間分を保存する。`GET /deploy/:id/metrics`、`/projects/:id/metrics`、`/projects/:id/tags/:tag/metrics`(`range`=1h/6h/24h)。仕様は [dev-docs/endpoints/tags.md](dev-docs/endpoints/tags.md)。画面は、構成図のカードとタグ一覧、「タグ」「メトリクス」タブ。

- **API トークンと webhook**: 機械(CI、スクリプト)が API を呼ぶための JWT。**backend の中で発行・検証する**(HS512。秘密は環境変数 `API_TOKEN_SECRET`、64 文字以上(`openssl rand -hex 64`)。**空なら機能は無効**)。接頭辞 `lkt_`。claim `scope`(`read`、`write`、`deploy:image`)と、`deploys`、`projects`(対象の絞り込み)を持ち、**DB(`api_tokens`)で管理するのは有効・無効だけ**。発行・一覧・失効(`/api-tokens`)は、ログインした利用者だけ(API トークンでは不可)。認証のミドルウェアが、`lkt_` で振り分ける(`middlewares.NewTokenDispatchMiddleware`、権限の判定は `shared/apitoken`)。**deploy のイメージは `PUT /deploy/:id/image` で更新できる**(`pending_image_ref`。起動設定の pending と一緒に作り、反映は `config/apply` と同じ流れ)。**イメージの更新履歴は `GET /deploy/:id/image-history`**(`deploy_image_history`。反映の結果を確定するとき、成功も失敗も 1 件足す。画面は deploy の概要の「イメージ」)。**webhook は `POST /hooks/deploy`**(トークンはヘッダー `X-Launchs-Token`。`deploy:image` で deploy を 1 つに絞ったトークン。ボディは `imageRef`、`tag`、Docker Hub の形式)。画面は、アカウント設定の「API トークン」と、deploy の概要の「自動デプロイ(webhook)」。設計は [dev-docs/api-tokens.md](dev-docs/api-tokens.md)、仕様は [dev-docs/endpoints/api_tokens.md](dev-docs/endpoints/api_tokens.md)。**通しの試験は、まだ**。
- **開発者向けの API ガイドは `dev-docs/api/`**(ジャンルごとに md を分けた入口。認証、共通のルール、プロジェクト、deploy、ネットワーク、ストレージ、タグ、テンプレート、webhook。**仕様の正本は `dev-docs/endpoints/`** で、ここは流れと例と落とし穴。エンドポイントや挙動を変えたら、`dev-docs/endpoints/`、`dev-docs/api/`、`docs/api.md` を直す)。
- **プロジェクトのタスク(Workflow の履歴)**: worker の interceptor(`worker/src/tracking`)が、全ての Workflow の開始と終了を `project_tasks` に記録する(失敗の理由は、秘密を伏せて保存)。`GET /projects/:projectId/tasks`、画面の「タスク」タブ。保存は 30 日と、プロジェクトごとに 1000 件。設計は [dev-docs/tasks.md](dev-docs/tasks.md)、仕様は [dev-docs/endpoints/tasks.md](dev-docs/endpoints/tasks.md)。
- **本番向けのマニフェストは `deploy/`(kustomize)**。PostgreSQL、Temporal、auth、backend、worker、watcher、logcollector、accessstats、frontend、IngressRoute を、namespace `launchs` に置く。**設定(ConfigMap `launchs-config` = `deploy/settings/config.yaml`)は、本体に含まれ、Git の値が既定。環境ごとの値(`LAUNCHS_HOST`、`INGRESS_BASE_DOMAIN` など)は、Argo CD の Application の `kustomize.patches` で上書きする(IngressRoute の Host は、`LAUNCHS_HOST` から `replacements` で入る。`deploy/argocd.md`)。秘密(Secret `secret.yaml`。gitignore。雛形は `secret.example.yaml`)だけは、本体と別に、先に手で適用する**(`kubectl apply -k settings/`)。マイグレーションは、`migrate` イメージ(`migrations/dockerfile`)を、backend と auth の initContainer が流す。イメージは、`main` への push で、GHCR(`ghcr.io/launchs-org/v2/<名前>`。`sha-…` と `latest`)に出る(`.github/workflows/release.yml`)。そのあと、CI が、`deploy/kustomization.yaml` の `newTag` を、今回の `sha-…` に、`main` へ自動でコミットする(クラスターへの適用は、しない。手動の `kubectl apply -k .`)。通しの確認(イメージが出てから)は、まだ。更新・マイグレーション・バックアップの運用手順は `deploy/operation.md`(最初のデプロイと更新は `deploy/deploy.md`、Q&A は `deploy/questions.md`、構成は `deploy/description.md`)。**土台(Traefik とプラグイン、metrics-server、cloudflared)も、Argo CD の Application(`deploy/argocd/applications.yaml`。`traefik`、`launchs-cluster`、`launchs-prod`)で入る**(`deploy/cluster/`。Traefik の values は `deploy/cluster/traefik-values.yaml` で、プラグインのソースから `python3 traefik-plugins/render_helm_values.py --full` で作る。CI がずれを検査する)。

## dev-docs/endpoints/ — エンドポイント仕様

全エンドポイントの確定仕様(Method/Path/Header/Body/Response/Note)をまとめたドキュメント。

- 元々 `dev-docs/endpoints.md` 1ファイルにまとめていたが、ボリュームが大きくなりすぎたため、グループ(認証・プロジェクト・ビルド・デプロイ・サービス・イングレス・ボリューム)ごとにファイルを分割した。
- [dev-docs/endpoints/README.md](dev-docs/endpoints/README.md) が索引。各グループの仕様ファイルへのリンクを持つ。
- 全グループに共通するルール(パス、ID、日時、名前、エラー形式、404、排他制御と削除、ページング)は [dev-docs/endpoints/common.md](dev-docs/endpoints/common.md) にまとめてある。各グループの仕様書にはそのリンクを置き、同じ内容を繰り返さない。
- ここに書かれている内容が実装すべき仕様の正本。pending_* → apply/discard という非同期反映モデル(config/service/ingress/volumeの変更はまず pending として保存し、apply で初めて実際に反映される)が全体を通した設計方針なので、新しいエンドポイントを追記する際もこのパターンに合わせる。
- apply呼び出し時の処理順序は全グループで統一する: `pending_*` の値を使って**先に実際のインフラ(k8s/ルーティング設定/ストレージ)への反映を試み、その結果(成功/失敗)が確定してから**、正式な値(env/ports/rules/sizeMB等)を `pending_*` の内容で上書きし `pending_*` を null に戻す。正式な値は常に「最後に反映を試みた値」を表し、status の成功/失敗がその結果を表す。

## dev-docs/issues/ — Issue管理・レビュー

`dev-docs/endpoints/` で定義した各エンドポイントについて、実装前にテスト要件(正常系・異常系・境界値)まで含めて1本ずつ詳細設計レビューを行うための Issue 置き場。

- 以前は全エンドポイントの Issue をフラットな連番ファイルで管理していたが、見通しが悪くなったため、`dev-docs/endpoints/` と同じグループ単位でサブディレクトリを切り直した(例: `dev-docs/issues/auth/`, `dev-docs/issues/projects/`)。
- [dev-docs/issues/README.md](dev-docs/issues/README.md) が全体の索引であり、**グループごとのレビュー進捗チェックリストも兼ねる**。新しいグループのIssueを作る/完了させたら、このチェックリストを更新すること。
- 各グループフォルダには `README.md`(そのグループ内Issue一覧)と、連番付きの個別Issue(`001-post-xxx.md` 等)を置く。
- 個別Issueのフォーマットは以下の構成で統一する(詳細は既存ファイルを参照):
  1. カテゴリ / Method・Path
  2. 説明
  3. テスト要件(正常系・異常系・境界値)
  4. 仕様(`dev-docs/endpoints/` の該当箇所をベースにしたJSON例など)
  5. 参照(該当する `dev-docs/endpoints/*.md` へのリンク)
- **レビューは1グループずつ進める**。ユーザーが現在のグループのレビューを完了したと明言するまで、次のグループのIssueは作成しない。

## dev-docs/talks/ — 会話ログ・設計判断の記録

実装前の設計レビューで Claude とのやり取りの中で決まった重要な決定事項や、その経緯・理由を残しておく場所。`dev-docs/endpoints/`・`dev-docs/issues/` が「結論としての仕様」を書く場所なのに対し、`dev-docs/talks/` は「なぜそう決めたか」というメタな記録を書く場所という役割分担。

- [dev-docs/talks/README.md](dev-docs/talks/README.md) が索引。ファイル名は `YYYY-MM-DD-短い説明.md`。
- 区切りの良いタイミング(グループのレビューが一段落した、大きな方針転換があった等)で、その時点までの経緯をまとめて追記する。毎回の発言を逐一書き写すのではなく、後から読んで意思決定の背景が分かる要約にする。

## CLAUDE.md の更新について

このファイルは一度書いて終わりにせず、プロジェクトの状態が変わるたびに**定期的に見直して更新する**。具体的には以下のタイミングで更新を検討すること:

- `dev-docs/endpoints/`・`dev-docs/issues/` にグループを追加/再構成したとき(ディレクトリ構成や運用ルールの記述が古くなっていないか)
- ユーザーから新しい作業方針・制約(例: 「1グループずつレビューする」「force pushは事前確認する」)が示されたとき → ここに追記して以後のセッションでも引き継げるようにする
- `dev-docs/talks/` に新しいログを追加したとき → 必要なら「作業時の注意」や各セクションの記述も最新の状況に合わせて更新する
- バックエンド/フロントエンドの技術構成が決まった・変わったとき

## 作業時の注意

- `dev-docs/endpoints/*.md` と `dev-docs/issues/**/*.md` の間は相互にリンクを張っているため、ファイルを移動・リネームする際は相対パスのリンク切れがないか確認する。
- git 履歴の扱いについて: develop ブランチは一度 force push で過去の Issue 関連コミットを整理し直した経緯がある。force push や reset --hard など履歴を書き換える操作は、必ず事前にユーザーの明示的な承認を取ってから行う。

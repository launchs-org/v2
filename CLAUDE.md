# CLAUDE.md

このリポジトリで作業する際の案内。

## プロジェクト概要

launchs-org 用に、Kubernetes 上でのデプロイ管理を行うバックエンド/フロントエンドを作成するプロジェクト。詳細は [readme.md](readme.md) を参照。

- バックエンド: Golang + Echo + Gorm(PostgreSQL)
- フロントエンド: 未定
- インフラ: 開発環境 Podman(Docker)、本番環境 Kubernetes(オンプレ)

## ディレクトリ構成

```
backend/        バックエンド(Golang/Echo)。dockerfile と src/(コード)を持つ
watcher/        k8s の Pod と Deployment を観測し、実際の状態を DB に反映するコンテナ(docs/watcher.md)
auth/           認証(ユーザー登録・ログイン・トークンの発行・JWKS。docs/auth.md)
registry/ logcollector/   同じ形のコンテナ用フォルダ(これから作る)
shared/         バックエンド・ワーカー・watcher で共有するコード
migrations/     DB のマイグレーション(up のみ)
docs/
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

実装前に決めた技術方針は [docs/pre-implementation-checklist.md](docs/pre-implementation-checklist.md) に回答と決定を残している。実装時はまずそちらを確認すること。主な点は以下。

- 構成: 認証・バックエンド(API)・ワーカー(Temporal Worker)・レジストリ(自作)・ログ収集は別コンテナ。プロジェクトルート直下にそれぞれフォルダを分け、フォルダの直下に `dockerfile`、その下の `src/` にコード(`src/main.go` など)を置く。
- バックエンドとワーカーで共有するコード(DBのモデル、Workflow/Activity の名前と入出力の型)は `shared/` に置く(`src/` は挟まない)。
- Go モジュールはルートに `go.mod` を1つ置き、モジュール名は `launchs`。docker のビルドコンテキストはリポジトリルートにする(コンテナごとに Dockerfile で使うフォルダを選ぶ)。
- ビルド・テスト・実行は、全てコンテナの中で行う(ホストの Go は `go.mod` より古い)。開発のコマンドは `Taskfile.yml`(go-task)にまとめる。
- コードを書くときのルールは [docs/coding-conventions.md](docs/coding-conventions.md) に従う。特に次を守る。
  - **一文字の変数名は禁止**(ループの添字、レシーバーも含む)。
  - **ファイル名は役割が分かる名前にする**(`project_controller.go` のように層の名前を付ける)。
  - **全ての関数・メソッドに、詳細な日本語のコメントを書く**(処理の流れ、引数、戻り値、エラー、副作用)。
  - **関数の中は、行ごとにコメントを書く**(1つの文ごとに、直前の行へ日本語で。テストも同じ)。
  - マイグレーションは up だけ書く。
- Git の運用: `develop` から `feature/<短い説明>` のブランチを切り、細かい単位で作業し、プルリクエストで `develop` に入れる。マージは本人が行う。Claude は、依頼がない限り push やプルリクエストの作成をせず、マージもしない。
- コンテナ名は `backend`(または `app`)、`worker` など。コンテナの名前と、コンテナ内部のコード構成は別の話として扱う。
- コンテナ内部のコード構成は `controller / service / repository / model` の層でファイルを分ける。
- **k8s の実際の状態(Pod、Deployment のレプリカ数など)を DB に書くのは、watcher だけ**(詳細は [docs/watcher.md](docs/watcher.md))。watcher は同時に1つだけ動かし、リーダー選出は PostgreSQL の advisory lock(`shared/leader`)で行う。
- **プロジェクトの namespace は、NetworkPolicy で隔離する**(namespace の作成時に適用。詳細は [docs/network-isolation.md](docs/network-isolation.md))。外からの通信を中継する namespace(Traefik など)は、環境変数 `INGRESS_NAMESPACE` で許可する。
- **ingress は、Traefik の IngressRoute として作る**(プロジェクトに属する。ホスト名は「形容詞-色-動物.ベースドメイン」で自動で払い出す。詳細は [docs/ingress.md](docs/ingress.md))。ベースドメインと払い出さない名前は、環境変数 `INGRESS_BASE_DOMAIN`、`INGRESS_RESERVED_SUBDOMAINS`。
- **volume は、k8s の PVC として作る**(プロジェクトに属する。PVC は、最初にマウントを適用するときに作る。詳細は [docs/volume.md](docs/volume.md))。大きさの上限、StorageClass、アクセスモードは、環境変数 `VOLUME_MAX_SIZE_MB`、`VOLUME_STORAGE_CLASS`、`VOLUME_ACCESS_MODE`。
- **状態による操作の可否と、状態を変える操作は、`shared/model` のメソッドに置く**(repository は、ロックして読む → メソッドを呼ぶ → 保存する、だけを行う。[docs/coding-conventions.md](docs/coding-conventions.md))。
- **認証は、別のコンテナ(`auth/`)**。URL は `/auth/...`(パスを書き換えずに渡す)。access token は EdDSA で、backend が JWKS の公開鍵で検証する(backend の置き換えは、次の PR)。クォータは、設計の文書 [docs/auth.md](docs/auth.md) の §8(未実装)。
- 責務分担: controller が pending の保存や即時反映系の書き込みを行い、非同期の apply は Temporal の Workflow/Activity が実行して結果確定後に正式値と status を更新する。

## docs/endpoints/ — エンドポイント仕様

全エンドポイントの確定仕様(Method/Path/Header/Body/Response/Note)をまとめたドキュメント。

- 元々 `docs/endpoints.md` 1ファイルにまとめていたが、ボリュームが大きくなりすぎたため、グループ(認証・プロジェクト・ビルド・デプロイ・サービス・イングレス・ボリューム)ごとにファイルを分割した。
- [docs/endpoints/README.md](docs/endpoints/README.md) が索引。各グループの仕様ファイルへのリンクを持つ。
- 全グループに共通するルール(パス、ID、日時、名前、エラー形式、404、排他制御と削除、ページング)は [docs/endpoints/common.md](docs/endpoints/common.md) にまとめてある。各グループの仕様書にはそのリンクを置き、同じ内容を繰り返さない。
- ここに書かれている内容が実装すべき仕様の正本。pending_* → apply/discard という非同期反映モデル(config/service/ingress/volumeの変更はまず pending として保存し、apply で初めて実際に反映される)が全体を通した設計方針なので、新しいエンドポイントを追記する際もこのパターンに合わせる。
- apply呼び出し時の処理順序は全グループで統一する: `pending_*` の値を使って**先に実際のインフラ(k8s/ルーティング設定/ストレージ)への反映を試み、その結果(成功/失敗)が確定してから**、正式な値(env/ports/rules/sizeMB等)を `pending_*` の内容で上書きし `pending_*` を null に戻す。正式な値は常に「最後に反映を試みた値」を表し、status の成功/失敗がその結果を表す。

## docs/issues/ — Issue管理・レビュー

`docs/endpoints/` で定義した各エンドポイントについて、実装前にテスト要件(正常系・異常系・境界値)まで含めて1本ずつ詳細設計レビューを行うための Issue 置き場。

- 以前は全エンドポイントの Issue をフラットな連番ファイルで管理していたが、見通しが悪くなったため、`docs/endpoints/` と同じグループ単位でサブディレクトリを切り直した(例: `docs/issues/auth/`, `docs/issues/projects/`)。
- [docs/issues/README.md](docs/issues/README.md) が全体の索引であり、**グループごとのレビュー進捗チェックリストも兼ねる**。新しいグループのIssueを作る/完了させたら、このチェックリストを更新すること。
- 各グループフォルダには `README.md`(そのグループ内Issue一覧)と、連番付きの個別Issue(`001-post-xxx.md` 等)を置く。
- 個別Issueのフォーマットは以下の構成で統一する(詳細は既存ファイルを参照):
  1. カテゴリ / Method・Path
  2. 説明
  3. テスト要件(正常系・異常系・境界値)
  4. 仕様(`docs/endpoints/` の該当箇所をベースにしたJSON例など)
  5. 参照(該当する `docs/endpoints/*.md` へのリンク)
- **レビューは1グループずつ進める**。ユーザーが現在のグループのレビューを完了したと明言するまで、次のグループのIssueは作成しない。

## docs/talks/ — 会話ログ・設計判断の記録

実装前の設計レビューで Claude とのやり取りの中で決まった重要な決定事項や、その経緯・理由を残しておく場所。`docs/endpoints/`・`docs/issues/` が「結論としての仕様」を書く場所なのに対し、`docs/talks/` は「なぜそう決めたか」というメタな記録を書く場所という役割分担。

- [docs/talks/README.md](docs/talks/README.md) が索引。ファイル名は `YYYY-MM-DD-短い説明.md`。
- 区切りの良いタイミング(グループのレビューが一段落した、大きな方針転換があった等)で、その時点までの経緯をまとめて追記する。毎回の発言を逐一書き写すのではなく、後から読んで意思決定の背景が分かる要約にする。

## CLAUDE.md の更新について

このファイルは一度書いて終わりにせず、プロジェクトの状態が変わるたびに**定期的に見直して更新する**。具体的には以下のタイミングで更新を検討すること:

- `docs/endpoints/`・`docs/issues/` にグループを追加/再構成したとき(ディレクトリ構成や運用ルールの記述が古くなっていないか)
- ユーザーから新しい作業方針・制約(例: 「1グループずつレビューする」「force pushは事前確認する」)が示されたとき → ここに追記して以後のセッションでも引き継げるようにする
- `docs/talks/` に新しいログを追加したとき → 必要なら「作業時の注意」や各セクションの記述も最新の状況に合わせて更新する
- バックエンド/フロントエンドの技術構成が決まった・変わったとき

## 作業時の注意

- `docs/endpoints/*.md` と `docs/issues/**/*.md` の間は相互にリンクを張っているため、ファイルを移動・リネームする際は相対パスのリンク切れがないか確認する。
- git 履歴の扱いについて: develop ブランチは一度 force push で過去の Issue 関連コミットを整理し直した経緯がある。force push や reset --hard など履歴を書き換える操作は、必ず事前にユーザーの明示的な承認を取ってから行う。

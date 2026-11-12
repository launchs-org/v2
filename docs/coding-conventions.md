# コーディング規約

コードの書き方とファイルの置き方。実装はこの規約に従う。変えるときは、このファイルを直す。

- **決定**: 回答をもらって決まったもの
- **採用案**: 指摘が無かったので、そのまま採用した案
- 前提とする決定は [実装前の確認項目](pre-implementation-checklist.md) と [共通ルール](endpoints/common.md)

---

## 1. リポジトリとフォルダの構成

**決定:**
- コンテナごとのフォルダをルート直下に置く。Dockerfile はそのフォルダの直下に、コードはその下の `src/` に置く。
- 共有コードのフォルダは `shared/`。
- Dockerfile のファイル名は、今のまま `dockerfile`(小文字)にする。

```
auth/                認証コンテナ
  dockerfile
  .air.toml
  src/               main.go と、controller/ service/ repository/ model/
backend/             API サーバー(auth/ と同じ形。今の backend/ がこの形)
worker/              Temporal の Worker
  dockerfile
  .air.toml
  src/               main.go と、workflow/ activity/
registry/            自作のレジストリ
logcollector/        ログ収集コンテナ
shared/              バックエンドとワーカーで共有するコード(src/ は挟まない)
migrations/          DB のマイグレーション(app/ と auth/ に分ける)
k8s/                 本番の k8s マニフェスト
dev/                 開発環境だけで使う設定(PostgreSQL の初期化など)
requests/            API の動作確認用のファイル(.http)
docs/
Taskfile.yml
go.mod, go.sum       ルートに1つ(モジュール名は `launchs`)
docker-compose.yaml
```

- インポートのパスには `src` が入る。例: `launchs/backend/src/controller`、`launchs/shared/model`。
- ビルドコンテキストはリポジトリルートにする(`shared/` を取り込むため)。Dockerfile がコンテナのフォルダの中にあっても、`docker-compose.yaml` では `context: .` と `dockerfile: backend/dockerfile` のように指定する。
- `shared/` の中身: `model/`(DB のモデル)、`repository/`(DB の読み書き)、`workflow/`(Workflow・Activity の名前、入出力の型、Task Queue 名、タイムアウトの定数)、`config/`、`logger/`、`apperror/`。
  - `repository` を共有にするのは、API(controller → service → repository)と、Worker の Activity の両方が同じテーブルを読み書きするため。
- 認証は別のデータベースなので、`auth/src/` の中に専用の `repository` と `model` を持つ。`shared/` からは使わない。
- Temporal 用のデータベースのスキーマは、Temporal の道具で作る。`migrations/` には含めない。
- 今の `backend/src/go.mod` は、ルートの `go.mod` に置き換える。`backend/src/main.go` の場所は変えない。

**air の設定:** ルートのモジュールにして `shared/` を共有するので、`shared/` の変更でもリビルドされる必要がある。そのため `.air.toml` は `<コンテナ>/.air.toml` に置き、リポジトリルートから `air -c backend/.air.toml` で実行する。`root = "."` にして、`include_dir` に `backend/src` と `shared` を入れる。ビルドは `go build -o ./tmp/backend ./backend/src`。`docker-compose.yaml` は、リポジトリ全体をバインドマウントする。今の `backend/src/.air.toml` は、この形に書き直す。`tmp/` は `.gitignore` に入れる(今は `backend/src/tmp/` だけなので、各コンテナの `tmp/` に広げる)。

---

## 2. Go の基本ルール

**決定:**
- モジュール名は、ローカルのままの短い名前 `launchs`(GitHub のパスは使わない)。
- ビルド・テスト・実行は、全てコンテナの中で行う(ホストの Go は 1.24.4 で、`go.mod` の 1.27.1 より古いため)。
- **一文字の変数名は禁止**。ループの添字、レシーバー、エラーも含めて、意味の分かる名前にする(`index`、`projectService`、`err` など)。
  - 例外は作らない。`*testing.T` も `testingT` のように書く(ルールが単純で、`varnamelen` でそのまま検出できるため)。
- **ファイル名は、役割が分かる名前にする**。層の名前を付けて、`project_controller.go`、`project_service.go`、`project_repository.go`、`project_model.go` のようにする。テストは `project_service_test.go`。
- 機械的に検出するため、`golangci-lint` の `varnamelen` を有効にする(設定は実装時に調整)。

**採用案:**
- パッケージ名は小文字・単数・短くする(`controller`、`service`、`repository`)。ファイル名は `snake_case`。
- 識別子は英語。略語は大文字にそろえる(`ProjectID`、`URL`、`JSON`)。
- interface は使う側のパッケージで定義し、小さくする。モックは手書きの fake にする(モックのライブラリは入れない)。
- 依存はコンストラクタで渡す。グローバル変数と `init()` は使わない。
- I/O をする関数は、第1引数に `ctx context.Context` を取る。
- `gofmt` と `goimports` をかける。import は「標準ライブラリ」「外部」「このモジュール」の3つに分ける。

---

## 3. 層のルール

**採用案:**
- 依存の向きは `controller → service → repository → model`。逆向きの参照は禁止。Worker は `workflow → activity → shared/repository`。
- **controller**: HTTP の入出力だけを書く。リクエストの bind と検証、レスポンスの組み立て、日時の JST 変換。ビジネスルールは書かない。
- **service**: ビジネスルール(状態遷移の検証、409 の判定、トランザクション)と、Temporal Client での Workflow 開始。
- **repository**: Gorm の操作だけ。ビジネスルールは書かない。
- **model**: DB の構造体と、`status` などの定数。API のリクエスト・レスポンスの構造体は、DB のモデルとは別に、controller に置く。DB のモデルをそのまま返さない。
- 他ユーザーのリソースを 404 にするため、repository の取得メソッドは、必ず `userID` を条件に含める。他人のものは、そもそも取得できない形にする。
- 409 の排他制御は、repository の専用メソッドで、`UPDATE ... WHERE status NOT IN ('applying', 'deleting')` のような条件付き更新にする。更新できた件数が0なら、service が 409 を返す。

---

## 4. エラー処理

**採用案:**
- `shared/apperror` に、種類を持つエラーを定義する。種類は、共通ルールのエラーの code に1対1で対応させる(`invalid_request`、`unauthorized`、`not_found`、`conflict`、`payload_too_large`、`too_many_requests`、`internal_error`)。
- service と repository は、このエラーを返す。controller のエラーハンドラーが、HTTP のステータスと `{"error": {"code": "...", "message": "..."}}` に変換する。
- 別のライブラリのエラーは `fmt.Errorf("...: %w", err)` で包んで、呼び出し元に返す。判定は `errors.Is` と `errors.As`。
- Gorm の `ErrRecordNotFound` は、repository の中で `not_found` に変換する。
- エラーのメッセージは、丁寧な文末にする(「保存できませんでした」。「保存できなかった」は使わない)。
- 利用者に返すメッセージに、SQL や k8s のエラーなどの内部情報を入れない。500 の詳細は、ログにだけ書く。
- `panic` は使わない(起動時の設定エラーを除く)。

---

## 5. ログ

**採用案:**
- 標準の `log/slog` の JSON ハンドラーで、標準出力に書く。
- 共通のフィールド: `component`、`request_id`、`user_id`、リソースの ID(`project_id`、`deploy_id` など)、`workflow_id`、`activity`。
- レベル: `Debug` は開発のみ、`Info` は状態の遷移とリクエストの完了、`Warn` は再試行できる失敗、`Error` は調査が必要な失敗。
- パスワード、トークン、環境変数の値(`DATABASE_URL` など)は、ログに出さない。
- リクエストのログは、Echo のミドルウェアで1リクエスト1行(method、path、status、所要時間、`request_id`)。
- `X-Request-Id` ヘッダを受け取る(無ければ生成する)。レスポンスにも返す。
- ロガーは、コンストラクタで渡す(`slog.Logger`)。`With` でフィールドを足して使う。

---

## 6. 設定

**採用案:**
- 設定は環境変数だけ。`shared/config` に構造体と `Load()` を置く。必須の値が無ければ、起動時に失敗させる。
- 変数名は `UPPER_SNAKE_CASE`。例: `DATABASE_URL`、`TEMPORAL_ADDRESS`、`TEMPORAL_NAMESPACE`、`STORAGE_CLASS`、`RUNTIME_CLASS`。
- シークレットも環境変数で渡す(本番は k8s の Secret から)。ログに出さない。

---

## 7. DB(Gorm とマイグレーション)

**決定:**
- `status` の列は `text` + `CHECK` 制約にする(値を足すときに、マイグレーションで変えやすい)。
- マイグレーションには **up だけ**を書く。down のファイルは作らない。戻したいときは、新しいマイグレーションを足す。

**採用案:**
- テーブル名は複数形の `snake_case`(`projects`、`deploys`)。カラムも `snake_case`。主キーは `id`。外部キーは `<リソース>_id`。
- ID はアプリ側で作る。`<接頭辞>-` + UUID v4(`github.com/google/uuid`)。
- 全テーブルに `created_at` と `updated_at`(`timestamptz`)を持つ。
- 物理削除のみ。論理削除(`deleted_at`)は使わない。削除は Workflow が行い、`deleting` の行は削除が終わるまで残す。
- 現在値の列と `pending_*` の列は、同じ型にする。数値・文字列は普通の列、配列・マップは JSONB(`gorm.io/datatypes` の `datatypes.JSON`)。
- マイグレーションは `golang-migrate`。ファイルは `migrations/app/000001_create_projects.up.sql` のように、6桁の連番にする。1ファイルに1つの目的。`AutoMigrate` は使わない。
- トランザクションは service が開始する。`repository` に `Transaction(ctx, func(tx) error)` を用意する。
- 時刻は DB には `timestamptz`、Go では `time.Time` で扱う。API に出すときに JST(`time.FixedZone("JST", 9*3600)`)へ変換する。日本は夏時間が無いので、`FixedZone` にして、タイムゾーンのデータが無くても動くようにする。

---

## 8. API(Echo)

**採用案:**
- ルーティングは `/api/v1` のグループにまとめ、リソースごとに `RegisterXxxRoutes(group)` を用意する。認証は `/auth` を別のコンテナが受ける。
- JSON のキーは、仕様書のとおり。`camelCase` が基本で、`pending_*` は `snake_case` のまま。構造体のタグで明示する。
- リクエストとレスポンスの構造体は、エンドポイントごとに controller に置く(`CreateProjectRequest`、`CreateProjectResponse`)。
- 認証のミドルウェア: access token を、JWKS の公開鍵(EdDSA)で検証する。鍵はキャッシュし、知らない `kid` が来たら取得し直す。`type` が `"access"` 以外は 401。`sub` を `userID` として context に入れる。
- 日時は、共通のヘルパーで JST に変換して返す。
- ページングは、共通のヘルパーで `limit` と `cursor` を扱う。`cursor` は、作成日時と ID を base64url にしたもの。

**決定:** リクエストの入力値のチェックは `go-playground/validator` で書く(構造体のタグで書けて、Echo の公式ドキュメントでも使われているため)。API の動作確認(curl や REST Client)とは別の話。

---

## 9. Temporal

**採用案:**
- Workflow・Activity の名前、入出力の型、Task Queue 名、タイムアウトの定数は `shared/workflow/` に置く。実装は `worker/src/workflow/` と `worker/src/activity/`。
- 名前: Workflow は `ApplyConfigWorkflow`、Activity は `ApplyConfigToK8s`(動詞 + 対象)。
- Workflow ID は、決定済みの形式(`{操作}-{リソースID}-{uuid}`)を作るヘルパーを、`shared/workflow/` に置く。
- Workflow は決定的に書く。`time.Now`、乱数、直接の I/O は使わず、`workflow.Now` や `workflow.Sleep` を使う。I/O は全て Activity に出す。
- Activity は、依存を持つ struct のメソッドにする。繰り返し実行されても壊れないように、冪等に書く。長い処理は、ハートビートを送る。
- Workflow と Activity の入出力は、それぞれ struct 1つにする(項目を足しても互換を保てる)。
- 失敗したときに `failed` にする補償の Activity は、Workflow がキャンセルされた後でも実行できるよう、切り離した context で呼ぶ。

---

## 10. Docker と開発環境

**決定:**
- 本番イメージのベースは `gcr.io/distroless/static`(小さく、攻撃面が少ない。シェルが無いので、デバッグはログで行う)。
- 開発のコマンドは、`Makefile` ではなく `Taskfile.yml`(go-task)にまとめる。

**採用案:**
- Dockerfile は、マルチステージにする。`Develop`(air を使った開発用)、`Build`、本番用。今ある Dockerfile は `Develop` だけ。
- `docker-compose.yaml` の各サービスは、ビルドコンテキストをリポジトリルートにし、リポジトリ全体をバインドマウントする。
- Taskfile のタスクは、全て `docker compose` を呼び、コンテナの中で実行する。案: `task up`、`task down`、`task logs`、`task migrate`、`task test`、`task lint`、`task fmt`、`task build`。

**注意:** `task` コマンドは、このサーバーにまだ入っていない。入れる必要がある。

---

## 11. 静的解析と CI

**採用案:**
- `gofmt`、`goimports`、`go vet`、`golangci-lint`(設定は `.golangci.yml`。有効にするのは `errcheck`、`govet`、`staticcheck`、`unused`、`ineffassign`、`varnamelen` など)。
- 全ての関数・メソッドにコメントがあるかは、レビューで確認する。機械的に検出できる範囲は `revive` などで行う。
- CI は GitHub Actions。プルリクエストで、lint、`go test ./...`、各コンテナのビルドを実行する。
- コミット時のフック(pre-commit)は使わず、CI で守る。

---

## 12. Git とコメント

**決定:**
- 作業はブランチを切って、細かい単位で進める。プルリクエストを送り、マージは本人が行う。
  - ブランチは `develop` から `feature/<短い説明>` を切る。プルリクエストの先は `develop`。
  - AI(Claude)は、push とプルリクエストの作成を、依頼があったときだけ行い、マージはしない。
  - `gh` コマンドは、このサーバーにまだ入っていない。プルリクエストの作成に使うなら、入れる必要がある。
- **メソッドと関数のコメントは、詳細に書く**。公開・非公開を問わず、全ての関数とメソッドに、日本語で書く。「何をするか」「処理の流れ」「引数」「戻り値」「エラー」「副作用(DB、Temporal、k8s)」を書く。例:

```go
// CreateProject は、ログイン中のユーザーのプロジェクトを新しく作成する。
//
// 処理の流れ:
//  1. name の形式(英数字とハイフン、64文字以内)を検証する。
//  2. 同じユーザーの中で name が重複していないか確認する。
//  3. プロジェクトを DB に保存する。
//
// 引数:
//   - ctx: リクエストの context。キャンセルとタイムアウトが伝わる。
//   - userID: プロジェクトを作成するユーザーの ID。
//   - name: プロジェクト名。
//
// 戻り値:
//   - 作成したプロジェクト。
//   - エラー。name が不正なら invalid_request、重複していれば conflict を返す。
//
// 副作用: projects テーブルへの INSERT。
func (projectService *ProjectService) CreateProject(ctx context.Context, userID string, name string) (*model.Project, error) {
```

**採用案:**
- コミットメッセージは日本語で、1〜2文で「なぜ」を書く(今の履歴のスタイル)。1コミットに1つの目的。
- 識別子は英語。

---

## 13. テストと動作確認

テストの方針(DB・k8s・Temporal をどう扱うか)は、実装が進んでから決める(確認項目 J-2)。

**決定:**
- API の動作確認は、curl のスクリプトか、VS Code の REST Client 拡張用のファイル(`.http`)で行う。

**採用案:**
- 動作確認のファイルは `requests/` に置き、グループごとに分ける(`requests/auth.http`、`requests/projects.http` など)。各ファイルの先頭に `@baseUrl` を書く。REST Client を使うので、`.http` を基本にし、curl のスクリプトは必要になったときだけ足す。
- テストは `xxx_test.go` として、同じフォルダに置く。標準の `testing` と `net/http/httptest` を使う。
- テーブル駆動にして、`t.Run` の名前には、Issue のテスト要件の文言を使う。例: `t.Run("既に使用されている userName は 409", ...)`。要件との対応が分かるようにする。

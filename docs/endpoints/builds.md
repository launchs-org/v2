[← エンドポイント一覧](README.md)

## ビルド関連 (/upload, /build)
### アップロード
- Post: /upload
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Request
- zip でボディを送信する (MAX100MB)
- Response (200)
- Note: 30分間有効なAssetトークンを吐き出す これをビルドに渡してダウンロードを行う
- Note: zipとして不正なファイル(拡張子/Content-Typeを偽装していても実体が壊れている、zip形式として解釈できない、不正なパスを含むエントリがある等)は400で拒否する
```json
{
  "assetsToken": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsImFzc2V0SWQiOiJhc3NldC0wMmVlYjFmOC0yODRlLTRiZGItODUxNi1jM2QzMzdlMzk0NjAiLCJzaGEyNTYiOiI5ODM0ODc2RENGQjA1Q0IxNjdBNUMyNDk1M0VCQTU4QzRBQzg5QjFBREY1N0YyOEYyRjlEMDlBRjEwN0VFOEYwIiwidHlwZSI6ImFzc2V0In0.6YQ16LRR0E5JcTIFdFrBcsttm6ki0HWmgmju5irGo9XJWkcCEXqD7MXmVQ7II_Y2ydA29yCwp96VIvX2k78_3g"
}
```

### ビルド (イメージ作成)
- Post: /build
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJhY2Nlc3MifQ.EO1DFWzMQfJJf66tm897rDFMN2GbeSPT8-Q1jw5wE1HgNaeD_rgctCJ5J2ZHSTPo4Ar7KzcH2fdyLEawkCC9Hw"
}
```
- Body (asset の場合)
```json
{
  "projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "sourceType": "asset",
  "asset": {"assetsToken": "<upload発行のトークン>"},
  "buildTargetDirectory": "packages/api",
  "buildMethod": "dockerfile"
}
```
- Body (git の場合、public repoのみ対応)
```json
{
  "projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "sourceType": "git",
  "git": {"repoUrl": "https://github.com/user/repo.git", "ref": "main"},
  "buildTargetDirectory": "packages/api",
  "buildMethod": "railpack"
}
```
- Response (200)
```json
{
  "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx"
}
```
- Note: build は projectId に属する。指定した projectId 配下の履歴として記録される
- Note: ビルドは非同期で開始される。buildId は状況追跡用。imageRef はこの時点で確定・払い出され以後不変だが、実際にpullしてデプロイに使えるのは status が "success" になってから(ビルド状況取得は別endpointとして別途設計)
- Note: imageRef のタグ部分は buildId を使う(例: project-xxxx:build-xxxx)。"latest" のような固定タグは使わず、ビルドごとに一意なタグにすることで、後続のビルドが以前のイメージを上書きしないようにする
- Note: sourceType が git の場合、現時点では public リポジトリのみ対応(private認証は未対応・将来対応)
- Note: buildTargetDirectory は任意項目。sourceType(asset/git)によらず共通で、ソースツリー内のビルド対象ディレクトリを指定する(monorepo対応)。省略時はルート直下を対象とする
- Note: buildMethod は必須項目("dockerfile" | "railpack")。省略した場合は自動判定を行わず 400 を返す
- Note: buildTargetDirectory に存在しないパスや、repoUrl/refが不正・存在しない場合、専用の事前検証は設けない。リクエスト自体は200で受理し、実際のビルド処理で対象が見つからず自然にstatusが"failed"になる

### ビルド状態取得
- Get: /build/:buildId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "status": "running",
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx"
}
```
- Note: status は "pending" | "running" | "canceling" | "success" | "failed" | "canceled" のいずれか
- Note: imageRef は [ビルド作成](/build) の時点で確定し、以後不変(build成功を待たずに参照可能。ただし実際にpullできるのは status が "success" になった後)

### ビルドログ取得
- Get: /build/:buildId/logs
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query (任意)
```json
{"since": 128}
```
- Response (200)
```json
{"logs": "Step 1/5 : FROM golang:1.27\n...", "nextSince": 256}
```
- Note: ストリーミングではなくポーリング前提。since を省略すると先頭からの全ログ、直前のレスポンスの nextSince を since に渡すと差分のみ返る
- Note: status が "running" の間は繰り返しポーリングすることで最新ログを追える。status が "success" | "failed" | "canceled" になった後も同じ方式でログ全体を取得できる(failed で中断したビルドのログも取得可能)
- Note: since に現在のログ末尾を超える値を指定した場合はエラーにせず、logs は空文字列、nextSince は現在のログ末尾の位置を返す(クライアントは次回もそのまま since に使い続けられる)

### ビルドキャンセル
- Post: /build/:buildId/cancel
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: status が "pending" または "running" の場合のみ有効。呼び出すと即座に status は "canceling" になり、実際のビルドプロセス中断が完了すると "canceled" に遷移する(この時点ではまだ "canceled" になっていない)
- Note: "canceling" への遷移後、中断が完了するより先にビルドが自然に成功/失敗した場合は、キャンセル自体が不成立になり、先に確定した "success"/"failed" が最終 status になる(完了済みのビルドを後からキャンセル扱いにはしない)
- Note: 既に "canceling" | "success" | "failed" | "canceled" の場合は 409 を返す(多重キャンセル不可)

[← ビルド関連](README.md)

# ビルド (イメージ作成)

- カテゴリ: ビルド関連 (/upload, /build)
- Method / Path: `Post /build`

## 説明

アップロード済みの asset または public な git リポジトリをソースに、コンテナイメージをビルドするエンドポイント。ビルドは非同期で実行され、`imageRef` はこの時点で確定・払い出されるが、実際に pull できるのはビルドが成功してから。ビルドは k8s の Job の中で、clone またはダウンロード+展開 → 準備 → ビルド → レジストリへ push の順に実行される。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `sourceType: "asset"` で有効な `assetsToken` を指定すると 200 が返り、`buildId`/`imageRef` が発行されること
- `sourceType: "git"` で public リポジトリの `repoUrl`/`ref` を指定すると 200 が返り、`buildId`/`imageRef` が発行されること
- `buildMethod: "dockerfile"` / `"railpack"` のどちらでもビルドが開始できること
- `buildTargetDirectory` を指定すると、そのサブディレクトリを対象にビルドされること(monorepo対応)
- `buildTargetDirectory` を省略した場合、ソースツリーのルート直下が対象になること
- 発行された `buildId` で [ビルド状態取得](003-get-build-buildid.md) をポーリングし、`status` が `"success"` になった時点で `imageRef` が pull 可能になること
- `imageRef` のタグ部分が `buildId` を使った一意な値になっており、同一プロジェクトへの複数回ビルドで以前のイメージを上書きしないこと

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `status` が `"deleting"` のプロジェクトを `projectId` に指定した場合 409 が返ること
- `buildMethod` を省略した場合、自動判定されず 400 が返ること
- `sourceType: "asset"` で期限切れ・不正な `assetsToken` を指定した場合エラーになること
- `sourceType: "git"` で private リポジトリを指定した場合エラーになること(現時点では public のみ対応)
- `sourceType` に `"asset"`/`"git"` 以外の値を指定した場合 400 が返ること

### 境界値
- `buildTargetDirectory` に存在しないパスや、`repoUrl`/`ref` が不正・存在しない場合、事前の検証エンドポイントは用意しない。リクエスト自体は 200 で受理し、実際のビルド処理(展開/clone)で対象が見つからず自然に `status: "failed"` になることで表現する

## 仕様

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
- Note: ビルドは k8s の Job の中で、clone またはダウンロード+展開 → 準備 → ビルド → レジストリへ push の順に実行される(API の挙動は変わらない)
- Note: deleting 中のプロジェクトを指定した場合は 409 を返す
- Note: ビルドは非同期で開始される。buildId は状況追跡用。imageRef はこの時点で確定・払い出され以後不変だが、実際にpullしてデプロイに使えるのは status が "success" になってから([ビルド状態取得](003-get-build-buildid.md))
- Note: imageRef のタグ部分は buildId を使う(例: project-xxxx:build-xxxx)。"latest" のような固定タグは使わず、ビルドごとに一意なタグにすることで、後続のビルドが以前のイメージを上書きしないようにする
- Note: sourceType が git の場合、現時点では public リポジトリのみ対応(private認証は未対応・将来対応)
- Note: buildTargetDirectory は任意項目。sourceType(asset/git)によらず共通で、ソースツリー内のビルド対象ディレクトリを指定する(monorepo対応)。省略時はルート直下を対象とする
- Note: buildMethod は必須項目("dockerfile" | "railpack")。省略した場合は自動判定を行わず 400 を返す
- Note: buildTargetDirectory に存在しないパスや、repoUrl/refが不正・存在しない場合、専用の事前検証は設けない。リクエスト自体は200で受理し、実際のビルド処理で対象が見つからず自然にstatusが"failed"になる

## 参照

- [dev-docs/endpoints/builds.md](../../endpoints/builds.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

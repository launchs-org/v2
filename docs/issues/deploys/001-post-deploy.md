[← デプロイ・起動設定関連](README.md)

# デプロイ (イメージから起動)

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Post /deploy`

## 説明

[ビルド (イメージ作成)](../builds/002-post-build.md) で生成された `imageRef` を指定して、コンテナを起動するエンドポイント。sourceType(asset/git)には関知せず、常に `imageRef` のみを受け取る。

## テスト要件

### 正常系
- 自分が所有する `projectId` と、そのプロジェクト配下で成功した `imageRef` を指定すると 200 が返り、`deployId` が発行されること
- 作成直後、[デプロイ詳細取得](002-get-deploy-deployid.md) で `status` が確認できること
- 作成直後、[プロジェクト deploy 履歴一覧](../projects/005-get-projects-projectid-deploys.md) にそのdeployが含まれること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `imageRef` が未指定の場合 400 が返ること
- `status` が `"success"` になっていない(pull不可能な)`imageRef` を指定した場合エラーになること(具体的な挙動は未設計のため別途定義する)
- `projectId` に属さない(他プロジェクトでビルドされた)`imageRef` を指定した場合はエラーになること

### 境界値
- 同一 `imageRef` から複数回 deploy を作成した場合、それぞれ独立した `deployId` として扱われること(1 imageRef = 複数 deploy 可)

## 仕様

- Post: /deploy
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJhY2Nlc3MifQ.EO1DFWzMQfJJf66tm897rDFMN2GbeSPT8-Q1jw5wE1HgNaeD_rgctCJ5J2ZHSTPo4Ar7KzcH2fdyLEawkCC9Hw"
}
```
- Body
```json
{
  "projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx"
}
```
- Response (200)
```json
{"deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
```
- Note: sourceType(asset/git)には関知せず、常に imageRef のみを受け取ってデプロイする
- Note: deploy も projectId に属する。指定した projectId 配下の履歴として記録される

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)

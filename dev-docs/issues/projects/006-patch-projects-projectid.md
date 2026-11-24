[← プロジェクト関連](README.md)

# プロジェクト更新

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Patch /projects/:projectId`

## 説明

プロジェクトの `name` を変更するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 有効な `name` を指定すると 200 が返り、[プロジェクト取得](003-get-projects-projectid.md) で変更後の `name` が取得できること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `name` が未指定の場合 400 が返ること
- `name` に英数字とハイフン以外の文字を含む、または65文字以上の場合 400 が返ること
- 同じユーザーの中で他のプロジェクトと `name` が重複した場合 409 が返ること(仮)
- プロジェクトの status が `"deleting"` の間は 409 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `name` が1文字・64文字の場合は更新できること(最小長の1文字は仮)

## 仕様

- Patch: /projects/:projectId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "my-app-renamed"}
```
- Response (200)
```json
{}
```
- Note: `name` の制約と重複の扱いは [プロジェクト作成](001-post-projects.md) と同じ。プロジェクトの status が "deleting" の間は 409

## 参照

- [dev-docs/endpoints/projects.md](../../endpoints/projects.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

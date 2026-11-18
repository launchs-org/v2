[← プロジェクト関連](README.md)

# プロジェクト作成

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Post /projects`

## 説明

新しいプロジェクトを作成するエンドポイント。以後の build/deploy は必ずいずれかの `projectId` に属し、このエンドポイントで発行された `projectId` を指定して呼び出す。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 有効な `name` を指定すると 200 が返り、`projectId` が発行されること
- 作成直後、[プロジェクト一覧](002-get-projects.md) にそのプロジェクトが含まれること
- 作成直後、[プロジェクト取得](003-get-projects-projectid.md) で同じ `name` が取得できること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `name` が未指定の場合 400 が返ること
- `name` に英数字とハイフン以外の文字を含む場合 400 が返ること
- `name` が65文字以上の場合 400 が返ること
- 同じユーザーの中で `name` が重複した場合 409 が返ること(仮)

### 境界値
- `name` が1文字・64文字の場合は作成できること(最小長の1文字は仮)
- `name` が空文字の場合 400 が返ること(仮)
- 1ユーザーあたりのプロジェクト作成上限数(仕様未確定のため別途定義する)

## 仕様

- Post: /projects
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "my-app"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
```
- Note: build/deploy は必ずいずれかの projectId に属する。以後の /build /deploy は projectId を伴って呼び出す
- Note: `name` は英数字とハイフンのみ、1〜64文字(最小長は仮)。違反は 400。同じユーザーの中で重複した場合は 409(仮)

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

[← プロジェクト関連](README.md)

# プロジェクト作成

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Post /projects`

## 説明

新しいプロジェクトを作成するエンドポイント。以後の build/deploy は必ずいずれかの `projectId` に属し、このエンドポイントで発行された `projectId` を指定して呼び出す。

## テスト要件

### 正常系
- 有効な `name` を指定すると 200 が返り、`projectId` が発行されること
- 作成直後、[プロジェクト一覧](002-get-projects.md) にそのプロジェクトが含まれること
- 作成直後、[プロジェクト取得](003-get-projects-projectid.md) で同じ `name` が取得できること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `name` が未指定の場合 400 が返ること
- 同一ユーザー内で `name` が重複した場合の扱い(許可するかエラーにするか)は仕様未確定のため別途定義する

### 境界値
- `name` の最小長・最大長、使用可能文字種(仕様未確定のため別途定義する)
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

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)

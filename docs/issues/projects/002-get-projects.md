[← プロジェクト関連](README.md)

# プロジェクト一覧

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Get /projects`

## 説明

ログイン中のユーザーが所有するプロジェクトの一覧を取得するエンドポイント。

## テスト要件

### 正常系
- プロジェクトを1件も作成していない場合、空配列が返ること
- [プロジェクト作成](001-post-projects.md) 後、一覧に `projectId`/`name` が含まれること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 大量のプロジェクトを保持している場合のページングは仕様未確定のため別途定義する

## 仕様

- Get: /projects
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projects": [{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app"}]}
```

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)

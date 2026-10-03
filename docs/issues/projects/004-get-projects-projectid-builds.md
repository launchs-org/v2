[← プロジェクト関連](README.md)

# プロジェクト build 履歴一覧

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Get /projects/:projectId/builds`

## 説明

指定したプロジェクト配下の build 履歴を、作成日時の降順で取得するエンドポイント。

## テスト要件

### 正常系
- build を1件も実行していないプロジェクトの場合、空配列が返ること
- 複数回 build した場合、`createdAt` の降順(新しい順)で返ること
- 各要素に `buildId`/`status`/`imageRef`/`createdAt` が含まれること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- ページングは仕様未確定のため別途定義する

## 仕様

- Get: /projects/:projectId/builds
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "builds": [
    {
      "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "success",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T00:00:00Z"
    }
  ]
}
```
- Note: 作成日時の降順で返す(ページングは未設計・別途)

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)

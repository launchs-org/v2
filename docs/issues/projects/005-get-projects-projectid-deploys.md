[← プロジェクト関連](README.md)

# プロジェクト deploy 履歴一覧

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Get /projects/:projectId/deploys`

## 説明

指定したプロジェクト配下の deploy 履歴を、作成日時の降順で取得するエンドポイント。

## テスト要件

### 正常系
- deploy を1件も実行していないプロジェクトの場合、空配列が返ること
- 複数回 deploy した場合、`createdAt` の降順(新しい順)で返ること
- 各要素に `deployId`/`status`/`imageRef`/`createdAt` が含まれること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 削除済み deploy を一覧に含めるかどうかは仕様未確定のため別途定義する
- ページングは仕様未確定のため別途定義する

## 仕様

- Get: /projects/:projectId/deploys
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "deploys": [
    {
      "deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "active",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T00:00:00Z"
    }
  ]
}
```
- Note: 作成日時の降順で返す(削除済みdeployを含めるかは未設計・別途)。ページングは未設計・別途

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)

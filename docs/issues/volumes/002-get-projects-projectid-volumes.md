[← ボリューム関連](README.md)

# ボリューム一覧

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Get /projects/:projectId/volumes`

## 説明

プロジェクト配下のボリューム一覧を取得するエンドポイント。

## テスト要件

### 正常系
- ボリュームを1件も作成していないプロジェクトの場合、空配列が返ること
- [ボリューム作成](001-post-projects-projectid-volumes.md) 後、一覧に `volumeId`/`name`/`sizeMB`/`pending_sizeMB` が含まれること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- ページングは仕様未確定のため別途定義する

## 仕様

- Get: /projects/:projectId/volumes
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "volumes": [
    {
      "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "name": "db-data",
      "pending_sizeMB": null,
      "sizeMB": 5120
    }
  ]
}
```

## 参照

- [docs/endpoints/volumes.md](../../endpoints/volumes.md)

[← ボリューム関連](README.md)

# ボリューム一覧

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Get /projects/:projectId/volumes`

## 説明

プロジェクト配下のボリューム一覧を取得するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- ボリュームを1件も作成していないプロジェクトの場合、空配列が返ること
- [ボリューム作成](001-post-projects-projectid-volumes.md) 後、一覧に `volumeId`/`name`/`status`/`sizeMB`/`pending_sizeMB` が含まれること
- `limit` を指定すると、その件数以内で返り、続きがあれば `nextCursor` が入ること
- `nextCursor` を `cursor` に渡すと次のページが返り、重複や欠落がないこと
- 最後のページで `nextCursor` が `null` であること
- 並び順が作成日時の降順であること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 範囲外の `limit`(0、101 など)・不正な `cursor` は 400 が返ること(仮)

### 境界値
- `limit` は1〜100(デフォルト20)。1と100は許可されること
- `status` が `"deleting"` のボリュームも一覧に含まれ、`status` がそのまま返ること

## 仕様

- Get: /projects/:projectId/volumes
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query(どちらも任意)
```json
{"limit": 20, "cursor": "<前回の nextCursor>"}
```
- Response (200)
```json
{
  "volumes": [
    {
      "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "name": "db-data",
      "status": "active",
      "pending_sizeMB": null,
      "sizeMB": 5120
    }
  ],
  "nextCursor": null
}
```
- Note: 並び順は作成日時の降順。`limit` は1〜100(デフォルト20)、範囲外の `limit` と不正な `cursor` は 400(仮)。`nextCursor` は最後のページなら null、続きがあれば文字列。`cursor` はクライアントが解釈しない文字列として扱う
- Note: status はボリューム自体の状態。"applying"(拡張の apply 中)| "active" | "failed"(拡張の反映に失敗)| "deleting"(削除中)。作成直後は "active"

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

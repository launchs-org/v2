[← プロジェクト関連](README.md)

# プロジェクト一覧

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Get /projects`

## 説明

ログイン中のユーザーが所有するプロジェクトの一覧を取得するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- プロジェクトを1件も作成していない場合、空配列が返ること
- [プロジェクト作成](001-post-projects.md) 後、一覧に `projectId`/`name`/`status` が含まれ、`status` が `"active"` であること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `limit` を指定すると、その件数以内で返り、続きがあれば `nextCursor` が入ること
- `nextCursor` を `cursor` に渡すと次のページが返り、重複や欠落がないこと
- 最後のページで `nextCursor` が null になること
- 範囲外の `limit`・不正な `cursor` は 400 が返ること(仮)

## 仕様

- Get: /projects
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
{"projects": [{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app", "status": "active"}], "nextCursor": null}
```
- Note: `status` はプロジェクトの状態。`"active"` は通常の状態、`"deleting"` は削除を受け付けて実体を削除している最中の状態
- Note: 作成日時の降順で返す。`limit` は1〜100(デフォルト20)。範囲外の `limit` と不正な `cursor` は 400(仮)。`cursor` はクライアントが解釈しない文字列で、`nextCursor` は最後のページなら null、続きがあれば文字列

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

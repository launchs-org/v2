[← イングレス関連](README.md)

# イングレス一覧

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Get /projects/:projectId/ingresses`

## 説明

プロジェクト配下のイングレス一覧を取得するエンドポイント。一覧では `ingressId`/`host` のみを返し、`rules`/`pending_rules`/`status` の詳細は [イングレス取得](003-get-projects-projectid-ingresses-ingressid.md) で個別に確認する。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- イングレスを1件も作成していないプロジェクトの場合、空配列が返り、`nextCursor` が `null` であること
- [イングレス作成](001-post-projects-projectid-ingresses.md) 後、一覧に `ingressId`/`host` が含まれること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `limit` を指定すると、その件数以内で返り、続きがあれば `nextCursor` が入ること
- `nextCursor` を `cursor` に渡すと次のページが返り、重複や欠落がないこと
- 最後のページで `nextCursor` が `null` であること
- 範囲外の `limit`(1〜100 の外)・不正な `cursor` は 400 が返ること(仮)
- 並び順は作成日時の降順であること

## 仕様

- Get: /projects/:projectId/ingresses
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
  "ingresses": [
    {
      "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "host": "random-word-1234.launchs.org"
    }
  ],
  "nextCursor": null
}
```
- Note: 一覧では ingressId/host のみを返す。rules/pending_rules/status の詳細は[イングレス取得](003-get-projects-projectid-ingresses-ingressid.md)で個別に確認する
- Note: 並び順は作成日時の降順。`limit` は1〜100(デフォルト20)、範囲外の `limit` と不正な `cursor` は 400(仮)。`cursor` はクライアントが解釈しない文字列。`nextCursor` は最後のページなら null、続きがあれば文字列

## 参照

- [dev-docs/endpoints/ingresses.md](../../endpoints/ingresses.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

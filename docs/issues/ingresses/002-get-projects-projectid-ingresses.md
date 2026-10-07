[← イングレス関連](README.md)

# イングレス一覧

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Get /projects/:projectId/ingresses`

## 説明

プロジェクト配下のイングレス一覧を取得するエンドポイント。一覧では `ingressId`/`host` のみを返し、`rules`/`pending_rules`/`status` の詳細は [イングレス取得](003-get-projects-projectid-ingresses-ingressid.md) で個別に確認する。

## テスト要件

### 正常系
- イングレスを1件も作成していないプロジェクトの場合、空配列が返ること
- [イングレス作成](001-post-projects-projectid-ingresses.md) 後、一覧に `ingressId`/`host` が含まれること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- ページングは仕様未確定のため別途定義する

## 仕様

- Get: /projects/:projectId/ingresses
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "ingresses": [
    {
      "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "host": "random-word-1234.launchs.org"
    }
  ]
}
```
- Note: 一覧では ingressId/host のみを返す。rules/pending_rules/status の詳細は[イングレス取得](003-get-projects-projectid-ingresses-ingressid.md)で個別に確認する

## 参照

- [docs/endpoints/ingresses.md](../../endpoints/ingresses.md)

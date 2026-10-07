[← イングレス関連](README.md)

# イングレス作成 (ランダムドメイン発行)

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Post /projects/:projectId/ingresses`

## 説明

project に属するイングレスを作成し、ランダムなサブドメイン(`host`)を自動発行するエンドポイント。deploy ではなく project に属する点に注意(複数の deploy を束ねて1つの ingress から振り分けられる)。作成直後は `rules` が空配列で、path rule の設定には別途 [イングレス更新](005-put-projects-projectid-ingresses-ingressid.md) を使う。

## テスト要件

### 正常系
- 自分が所有する `projectId` に対して呼び出すと 200 が返り、`ingressId`/`host` が発行されること
- 作成直後、`rules` が空配列、`pending_rules` が `null`、`status` が `"active"` であること
- 作成直後、[イングレス一覧](002-get-projects-projectid-ingresses.md) にそのイングレスが含まれること
- 発行される `host` がプロジェクトごと・呼び出しごとに一意であること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)

### 境界値
- 1プロジェクトあたりのイングレス作成数に上限は設けない(何個でも作成できる)
- `host` を指定して作成する手段は無い(常にランダム割り当て)こと

## 仕様

- Post: /projects/:projectId/ingresses
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [],
  "pending_rules": null,
  "status": "active"
}
```
- Note: ingress は project に属する(deployには属さない)。host はランダムなサブドメインが自動割り当てされる(host を指定して作成する形にはしない)
- Note: 対応プロトコルは HTTP のみ。TCP/UDP の公開は未対応(将来対応)
- Note: 作成直後は rules が空配列の状態で反映済み(status: "active")。path ruleを設定するには [イングレス更新](005-put-projects-projectid-ingresses-ingressid.md) を使う

## 参照

- [docs/endpoints/ingresses.md](../../endpoints/ingresses.md)

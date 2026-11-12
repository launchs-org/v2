[← サービス関連](README.md)

# サービス削除

- カテゴリ: サービス関連 (/deploy/:deployId/service)
- Method / Path: `Delete /deploy/:deployId/service`

## 説明

deploy に紐づく service を削除するエンドポイント。呼び出すと即座に `status` が `"deleting"` になり、実体(k8s Service等)の削除は非同期で行われる。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- service 作成済みの `deployId` に対して呼び出すと 200 が返り、`status` が即座に `"deleting"` になること
- 実体の削除が完了すると、以後 [サービス取得](002-get-deploy-deployid-service.md) が 404 を返すこと
- 削除時、当該 service の各ポートを参照している ingress の path rule が連鎖して削除されること(ルールの削除は呼び出し時点、つまり `status: "deleting"` になった時点で行われる)
- deploy が削除されるとき、その service も連鎖して削除され、`status` が `"deleting"` になること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- service が未作成の `deployId` に対して呼び出した場合 404 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 既に `status` が `"deleting"` の間に再度削除を呼び出した場合 409 が返ること(削除処理中の多重実行不可)
- deploy の `status` が `"deleting"` の間に呼び出した場合 409 が返ること

### 境界値
- 特になし

## 仕様

- Delete: /deploy/:deployId/service
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: 呼び出すと即座に応答し、service の status は直ちに "deleting" になる。実体(k8s Service等)の削除処理は非同期に開始され、完了するとレコード自体が削除される([サービス取得](002-get-deploy-deployid-service.md)は 404 を返すようになる)
- Note: 当該 service の各ポートを参照している ingress の path rule は、呼び出し時点(status: "deleting" になった時点)で連鎖して削除される
- Note: 既に status が "deleting" の間に再度削除を呼び出した場合は 409 を返す(削除処理中の多重実行不可)
- Note: deploy が削除されるときは、その service も連鎖して削除される(status が "deleting" になる)。この連鎖削除でも、上記の ingress の path rule の削除は同様に行われる
- Note: deploy の status が "deleting" の間にこの操作を呼び出した場合も 409 を返す

## 参照

- [docs/endpoints/services.md](../../endpoints/services.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

[← サービス関連](README.md)

# サービス取得

- カテゴリ: サービス関連 (/deploy/:deployId/service)
- Method / Path: `Get /deploy/:deployId/service`

## 説明

deploy に紐づく service の現在の状態を取得するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- service 作成済みの `deployId` を指定すると 200 が返り、`ports`/`pending_ports`/`status`/`dnsName`/`clusterIP` が取得できること
- `pending_ports` が存在する場合、その内容が返ること(無ければ `null`)

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- service が未作成の `deployId` に対して呼び出した場合 404 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 特になし

## 仕様

- Get: /deploy/:deployId/service
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ],
  "pending_ports": null,
  "status": "active",
  "dnsName": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.svc.cluster.local",
  "clusterIP": "10.43.12.7"
}
```
- Response (200、pending が存在する場合)
```json
{
  "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ],
  "pending_ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "game", "port": 27015, "protocol": "udp"}
  ],
  "status": "active",
  "dnsName": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.svc.cluster.local",
  "clusterIP": "10.43.12.7"
}
```
- Note: ports は現在反映されている内容。pending_ports は更新リクエスト後まだ apply されていない変更内容(変更が無ければ null)
- Note: サービスが未作成のdeployに対して呼び出すと 404 を返す

## 参照

- [docs/endpoints/services.md](../../endpoints/services.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

[← サービス関連](README.md)

# サービス作成 (ポート公開)

- カテゴリ: サービス関連 (/deploy/:deployId/service)
- Method / Path: `Post /deploy/:deployId/service`

## 説明

deploy のコンテナが持つポートをクラスタ内(および必要なら ingress から参照されて外部)へ公開するエンドポイント。1 deploy = 1 service で、サービスの作成は任意(全ての deploy が持つわけではない)。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 自分が所有する `deployId` に対して `ports`(各要素に `name`/`port`/`protocol`)を指定すると 200 が返り、`serviceId` が発行され、`status` が `"pending"` になること
- 作成直後は `dnsName`/`clusterIP` が `null` であり、`status` が `"active"` になった後に値が入ること
- `protocol` に `"tcp"`/`"udp"` の両方を指定できること
- 複数ポートを1つの `ports` 配列にまとめて指定できること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- deploy の `status` が `"deleting"` の間に service を作成しようとした場合 409 が返ること
- 既に service が存在する `deployId` に対して再度呼び出した場合 409 が返ること(1 deploy = 1 service)
- `ports` が未指定、または空配列の場合 400 が返ること
- `protocol` に `"tcp"`/`"udp"` 以外の値を指定した場合 400 が返ること
- 同一 `ports` 配列内で `name` が重複した場合 400 が返ること

### 境界値
- 同一 `ports` 配列内で、同じ `port` と `protocol` の組が重複した場合 400 が返ること。同じ `port` 番号でも `protocol` が違えば許可すること(DNS の 53 番など。決定)
- `ports` が 20 個なら成功し、21 個なら 400 が返ること(上限 20。決定)
- `name` は、小文字の英数字とハイフン、15文字まで。先頭・末尾のハイフン、連続するハイフン、英字を含まないもの(数字だけ)は 400 が返ること(k8s のポート名の規則)
- `port` 番号の範囲(1-65535)外を指定した場合 400 が返ること

## 仕様

- Post: /deploy/:deployId/service
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ]
}
```
- Response (200)
```json
{
  "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ],
  "pending_ports": null,
  "status": "pending",
  "dnsName": null,
  "clusterIP": null
}
```
- Note: service は deploy に1つだけ属する(1 deploy = 1 service)。既にserviceが存在するdeployに対して再度呼び出すと 409 を返す
- Note: deploy の status が "deleting" の間は 409 を返す(deploy の削除に伴い service も deleting になるため)
- Note: service はコンテナ内の複数ポートを、クラスタ内(および必要ならingressのpath ruleから参照されて外部)へ公開する単位。ports配列の各要素でport番号ごとにprotocol("tcp"|"udp")を選べる
- Note: HTTPという制約はここには無い(TCP/UDPどちらのポートも登録できる)。「HTTPのみ対応」の制約は[イングレス](../ingresses/README.md)側にあり、ingressのpath ruleはprotocol "tcp" のポートのみ参照できる
- Note: status は "pending" | "active" | "failed" | "deleting" のいずれか。dnsName(`<serviceId>.<projectId>.svc.<クラスターのドメイン>` の形)と clusterIP は "active" になるまで null(status/dnsName/clusterIP はサービス自体の払い出し状態であり、pending_*の対象ではない)

## 参照

- [docs/endpoints/services.md](../../endpoints/services.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

[← エンドポイント一覧](README.md)

## イングレス関連 (/projects/:projectId/ingresses)
### イングレス作成 (ランダムドメイン発行)
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
- Note: 作成直後は rules が空配列の状態で反映済み(status: "active")。path ruleを設定するには [イングレス更新](#イングレス更新-rules-一括置き換えpendingとして保存) を使う

### イングレス一覧
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
- Note: 一覧では ingressId/host のみを返す。rules/pending_rules/status の詳細は[イングレス取得](#イングレス取得)で個別に確認する

### イングレス取得
- Get: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080}
  ],
  "pending_rules": null,
  "status": "active"
}
```
- Response (200、pending が存在する場合)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080}
  ],
  "pending_rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080},
    {"path": "/api", "serviceId": "service-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "port": 8081}
  ],
  "status": "active"
}
```
- Note: rules は現在反映されているpath ruleの一覧。pending_rules は[イングレス更新](#イングレス更新-rules-一括置き換えpendingとして保存)後まだ[適用](#イングレス-適用)されていない変更内容(変更が無ければ null)
- Note: status は "applying" | "active" | "failed" のいずれか。applyを呼ぶと反映が完了するまで "applying" になる

### イングレス削除
- Delete: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: 配下の path rule(rules/pending_rules) も全て連鎖して削除される
- Note: ingress の status が "applying" の間は 409 を返す(進行中のapplyと排他制御する)

### イングレス更新 (rules 一括置き換え、pendingとして保存)
- Put: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080},
    {"path": "/api", "serviceId": "service-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "port": 8081}
  ]
}
```
- Response (200)
```json
{}
```
- Note: rules は個別rule単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)。path ruleの追加・削除・変更は全てこのエンドポイントで配列ごと送る
- Note: この時点ではまだ反映されない。pending_rules として保存され、[イングレス適用](#イングレス-適用) を呼ぶまで rules(現在反映されている値)は変わらない
- Note: serviceId には同一 project 内の任意の deploy に属する service を指定できる。path ごとに向き先の service を切り替えられる
- Note: port には対象serviceの ports 配列に含まれるポート番号を指定する。ingressはHTTPのみ対応のため、指定するportは対象serviceのports配列で protocol が "tcp" のものに限る("udp"のポートを指定した場合はエラーになる)
- Note: ingress の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はrulesに対する変更・apply・discardを排他制御する)

### イングレス 適用
- Post: /projects/:projectId/ingresses/:ingressId/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_rules の内容を rules に反映するため、ルーティング設定の書き換えを開始する。呼び出し自体は即座に応答するが、この時点ではまだ rules は更新されていない。pending_rules が null の場合は 409 を返す
- Note: 呼び出し直後、ingress の status は "applying" になる。この間 pending_rules はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_rules の値を使って先にルーティング設定への反映を試み、その結果(成功・失敗)が確定した時点で初めて rules を pending_rules の内容に更新し、pending_rules は null に戻す。成功すれば status は "active" に、失敗すれば "failed" になる(rules の内容自体は失敗時も新しい値で上書きされる)
- Note: ingressの反映はdeployのコンテナ再起動を伴わないルーティング設定の書き換えのみのため、config/volumes/serviceのapplyより短時間で完了する想定だが、確認手順(status のポーリング)は同様
- Note: 既に ingress の status が "applying" の場合は 409 を返す(多重apply不可)
- Note: applying には 60秒 のタイムアウトを設ける。この時間内に反映完了が確認できない場合、強制的に status を "failed" に遷移させる
- Note: pending_rules に含まれる serviceId/port の妥当性(同一project内のserviceであること、対象serviceのportsに存在するport番号であること、protocolが"tcp"であること)は、イングレス更新時点だけでなくapply実行時にも改めて検証する。更新から適用までの間にservice側のポート構成が変更される可能性があるため

### イングレス 破棄
- Post: /projects/:projectId/ingresses/:ingressId/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_rules の内容を適用せずに破棄する(rules には影響しない)。pending_rules が null の場合は 409 を返す
- Note: ingress の status が "applying" の間は 409 を返す

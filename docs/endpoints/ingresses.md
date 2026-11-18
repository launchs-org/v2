[← エンドポイント一覧](README.md)

## イングレス関連 (/projects/:projectId/ingresses)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/projects/:projectId/ingresses` の形)。

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
  "host": "random-word-1234.shconn.org",
  "rules": [],
  "pending_rules": null,
  "status": "active"
}
```
- Note: ingress は project に属する(deployには属さない)。host はランダムなサブドメインが自動割り当てされる(host を指定して作成する形にはしない)
- Note: 対応プロトコルは HTTP のみ。TCP/UDP の公開は未対応(将来対応)
- Note: ルーティングの反映は Traefik の IngressRoute(CRD)を更新して行う。Traefik までのリクエストの到達、ドメインの管理、TLS はこのシステムの管理外
- Note: 削除中("deleting")のプロジェクトにはイングレスを作成できず、409 を返す
- Note: host は「形容詞-色-動物」の3つの単語を、ベースドメイン(環境変数 INGRESS_BASE_DOMAIN)の前につなげたもの。払い出さない名前は、環境変数 INGRESS_RESERVED_SUBDOMAINS で決める([docs/ingress.md](../ingress.md))
- Note: 作成直後は rules が空配列の状態で反映済み(status: "active")。IngressRoute は、rules が1つ以上あるときだけ k8s に存在する。path ruleを設定するには [イングレス更新](#イングレス更新-rules-一括置き換えpendingとして保存) を使う

### イングレス一覧
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
      "host": "random-word-1234.shconn.org"
    }
  ],
  "nextCursor": null
}
```
- Note: 一覧では ingressId/host のみを返す。rules/pending_rules/status の詳細は[イングレス取得](#イングレス取得)で個別に確認する
- Note: 並び順は作成日時の降順。`limit` は1〜100(デフォルト20)、範囲外の `limit` と不正な `cursor` は 400(仮)。`cursor` はクライアントが解釈しない文字列。`nextCursor` は最後のページなら null、続きがあれば文字列

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
  "host": "random-word-1234.shconn.org",
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080}
  ],
  "pending_rules": null,
  "status": "active",
  "accessStats": true
}
```
- Response (200、pending が存在する場合)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.shconn.org",
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
- Note: accessStats は、アクセスの集計(Traefik のプラグイン launchs-stats。[docs/ingress-plugins.md](../ingress-plugins.md))が有効か(boolean。既定 true。作成・取得・切り替えのレスポンスに含まれる)
- Note: status は "applying" | "active" | "failed" | "deleting" のいずれか。applyを呼ぶと反映が完了するまで "applying" になる。削除を受け付けると "deleting" になる

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
- Note: 呼び出すと即座に応答し、status は直ちに "deleting" になる。実体の削除は Workflow で非同期に行い、完了するとレコード自体が削除される(以後の GET は 404 を返す)
- Note: deleting の間は、変更系の操作と再度の削除は 409 を返す。GET は status が "deleting" の状態を返す

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
- Note: port には対象serviceの ports 配列に含まれるポート番号を指定する。ingressはHTTPのみ対応のため、指定するportは対象serviceのports配列で protocol が "tcp" のものに限る("udp"のポートを指定した場合は 400 になる)。service が削除中の場合も 400
- Note: ingress の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はrulesに対する変更・apply・discardを排他制御する)。"deleting" の間も 409 を返す

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
- Note: pending_rules の内容を rules に反映するため、ルーティング設定の書き換えを開始する。呼び出し自体は即座に応答するが、この時点ではまだ rules は更新されていない。pending_rules が null の場合は 409 を返す。ルーティング設定の書き換えは Traefik の IngressRoute(CRD)を更新して行う。Traefik までのリクエストの到達、ドメインの管理、TLS はこのシステムの管理外
- Note: 呼び出し直後、ingress の status は "applying" になる。この間 pending_rules はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_rules の値を使って先にルーティング設定への反映を試み、その結果(成功・失敗)が確定した時点で初めて rules を pending_rules の内容に更新し、pending_rules は null に戻す。成功すれば status は "active" に、失敗すれば "failed" になる。失敗したときは、rules は動いている値のまま変わらず(IngressRoute も元の rules に戻す)、pending_rules は試した値として残る。直して再度 PUT するか、discard できる(status が "failed" でも、PUT・apply・discard・削除ができる)
- Note: ingressの反映はdeployのコンテナ再起動を伴わないルーティング設定の書き換えのみのため、config/volumes/serviceのapplyより短時間で完了する想定だが、確認手順(status のポーリング)は同様
- Note: 既に ingress の status が "applying" の場合は 409 を返す(多重apply不可)。"deleting" の場合も 409 を返す
- Note: 反映は Workflow で行い、一時的な失敗はリトライする。リトライの期限までに成功しなければ、IngressRoute を元の rules に戻して、status を "failed" にする(applying のまま固まらない)
- Note: pending_rules に含まれる serviceId/port の妥当性(同一project内のserviceであること、対象serviceのportsに存在するport番号であること、protocolが"tcp"であること)は、イングレス更新時点だけでなくapply実行時にも改めて検証する。更新から適用までの間にservice側のポート構成が変更される可能性があるため(apply 時に不正なら 409)。service の削除やポートの削減で、ingress の rules が自動で消えることはない(流す先を失った rules は、次の apply で 409 になる)

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
- Note: ingress の status が "applying" または "deleting" の間は 409 を返す

### イングレス アクセスの集計の切り替え
- Patch: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"accessStats": true}
```
- Response (200): [イングレス取得](#イングレス取得)と同じ形(`accessStats` を含む)
- Note: `accessStats`(必須。省略は 400)は、Traefik のプラグイン launchs-stats による、アクセスの集計を使うか。既定は true。取得とレスポンスにも `accessStats` を含む
- Note: pending ではなく、即時に保存する。反映済みのルールがあれば、IngressRoute を作り直す Workflow を開始する(Middleware の付け外し。同じ値でも開始する)
- Note: worker の環境変数 `INGRESS_STATS_PLUGIN` が空の環境では、保存だけで、IngressRoute には何も付かない
- Note: 他人の ingress は 404、applying / deleting は 409

### イングレス アクセスの集計の取得
- Get: /projects/:projectId/ingresses/:ingressId/access-stats?range=1h
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query: `range` は `1h`(既定。1 分ごと)、`24h`(15 分ごと)、`7d`(1 時間ごと)、`30d`(1 日ごと)。それ以外は 400
- Response (200)
```json
{
  "enabled": true, "range": "1h", "from": "2026-11-18T09:00:00Z", "to": "2026-11-18T10:00:30Z", "bucketSeconds": 60,
  "totals": {"count": 10, "status": {"1xx": 0, "2xx": 9, "3xx": 0, "4xx": 0, "5xx": 1}, "methods": {"GET": 10}, "bytes": 500, "avgMs": 2, "p50Ms": 5, "p95Ms": 5, "p99Ms": 5},
  "series": [{"start": "2026-11-18T09:00:00Z", "count": 0, "status": {}, "methods": {}, "bytes": 0, "avgMs": 0, "p50Ms": 0, "p95Ms": 0, "p99Ms": 0}],
  "paths": [{"key": "/api/:id", "count": 10}],
  "clients": [{"key": "203.0.113.9", "count": 10}],
  "uniqueClients": 1
}
```
- Note: 集計は accessstats コンテナが、Traefik のログから保存する。1 分ごとの行は 24 時間、1 時間ごとの行は 30 日残る。`ACCESS_STATS_KEEP_CLIENTS=false` では、clients は空
- Note: 応答時間の p50/p95/p99 は、区切り(5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000 ms)の上限での見積もり。パスは、数字・UUID・長い 16 進の区間を `:id` にまとめ、上位だけを残す
- Note: 無効にした後も、過去の集計は読める。他人の ingress は 404

### アクセスの集計の切り替え
- Patch: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"accessStats": false}
```
- Response (200): [イングレス取得](#イングレス取得)と同じ形(更新後の値)
- Note: `accessStats`(boolean)は必須(無ければ 400)。保存したあと、反映済みのルールがあれば、IngressRoute(と、集計用の Middleware)を、現在のルールのまま作り直す Workflow を開始して、すぐ応答する(同じ値でも開始する。冪等)。status は変えない
- Note: 他のユーザーの ingress は 404。applying・deleting の間は 409
- Note: worker の環境変数 `INGRESS_STATS_PLUGIN` が空(既定)の環境では、値は保存されるが、k8s には何も反映されない

### アクセスの集計の取得
- Get: /projects/:projectId/ingresses/:ingressId/access-stats?range=1h
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query: `range` は `1h`(既定)、`24h`、`7d`、`30d`。それ以外は 400
- Response (200)
```json
{
  "enabled": true,
  "range": "1h",
  "from": "2026-11-18T09:30:00Z",
  "to": "2026-11-18T10:30:30Z",
  "bucketSeconds": 60,
  "totals": {
    "count": 22,
    "status": {"1xx": 0, "2xx": 22, "3xx": 0, "4xx": 0, "5xx": 0},
    "methods": {"GET": 16, "POST": 6},
    "bytes": 12098,
    "avgMs": 3.16,
    "p50Ms": 5, "p95Ms": 10, "p99Ms": 10
  },
  "series": [{"start": "2026-11-18T10:25:00Z", "count": 11, "status": {"1xx": 0, "2xx": 11, "3xx": 0, "4xx": 0, "5xx": 0}, "methods": {"GET": 8, "POST": 3}, "bytes": 6070, "avgMs": 3.1, "p50Ms": 5, "p95Ms": 10, "p99Ms": 10}],
  "paths": [{"key": "/api/items/:id", "count": 13}],
  "clients": [{"key": "198.51.100.7", "count": 8}],
  "uniqueClients": 8
}
```
- Note: 区間の長さは、`1h` が 60 秒、`24h` が 15 分、`7d` が 1 時間、`30d` が 1 日。`series` は、リクエストが無い区間も含めて、時刻の昇順で返す
- Note: `enabled` は、今の集計の設定(無効にしても、過去の集計は、保存期間(1 分ごとは 24 時間、1 時間ごとは 30 日)の間、読める)
- Note: `p50Ms` などは、応答時間の度数(区切り 5、10、25、50、100、250、500、1000、2500、5000 ミリ秒)からの見積もりで、その区切りの上限を返す
- Note: `paths` は、ID らしい区間(数字、UUID、16 文字以上の 16 進)を `:id` にまとめたパス。`clients` は、`X-Real-Ip`(launchs-edge が入っていれば、本当のクライアントの IP)。どちらも上位 20 件。`ACCESS_STATS_KEEP_CLIENTS=false` では、`clients` は空
- Note: 他のユーザーの ingress は 404

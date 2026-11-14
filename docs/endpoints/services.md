[← エンドポイント一覧](README.md)

## サービス関連 (/deploy/:deployId/service)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/deploy/:deployId/service` の形)。
### サービス作成 (ポート公開)
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
- Note: HTTPという制約はここには無い(TCP/UDPどちらのポートも登録できる)。「HTTPのみ対応」の制約は[イングレス](ingresses.md)側にあり、ingressのpath ruleはprotocol "tcp" のポートのみ参照できる
- Note: status は "pending" | "active" | "failed" | "deleting" のいずれか。dnsName と clusterIP は "active" になるまで null(status/dnsName/clusterIP はサービス自体の払い出し状態であり、pending_*の対象ではない)
- Note: **dnsName は `<serviceId>.<projectId>.svc.<クラスターのドメイン>`** の形。Kubernetes の Service の正式な名前は `<名前>.<namespace>.svc.<クラスターのドメイン>` で、namespace(projectId)は省略できない。クラスターのドメインは、クラスターごとに違い(例 `test-cluster.local`)、ワーカーの環境変数 `CLUSTER_DOMAIN` で指定する。同じプロジェクトの deploy からは、短い名前(`<serviceId>`)でも届く。**他のプロジェクトからは、隔離(NetworkPolicy)のため、届かない**([network-isolation.md](../network-isolation.md))
- Note: k8s の Service の種類は、ワーカーの環境変数 `SERVICE_TYPE`(`ClusterIP`(既定)、`NodePort`、`LoadBalancer`)で決まる。`LoadBalancer` は、deploy ごとに外部 IP を使い、利用者のポート(UDP を含む)を LAN に公開する。外部 IP が払い出されなくても、status は "active" になる(clusterIP は払い出される)
- Note: ports の検査: 1〜20個。name は小文字の英数字とハイフン(15文字まで。先頭・末尾のハイフンと、連続するハイフンは不可。英字を1つ以上含む)。port は 1〜65535。protocol は "tcp" | "udp"。**name の重複と、同じ port と protocol の組の重複は 400**。同じ port 番号でも、protocol が違えば許可する(DNS の 53 番など)

### サービス取得
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

### サービス更新 (一括置き換え、pendingとして保存)
- Patch: /deploy/:deployId/service
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "game", "port": 27015, "protocol": "udp"}
  ]
}
```
- Response (200)
```json
{}
```
- Note: ports は個別要素単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)。ポートの追加・削除・変更は全てこのエンドポイントで配列ごと送る
- Note: この時点ではまだ反映されない。pending_ports として保存され、サービスの apply を呼ぶまで ports(現在反映されている値)は変わらない
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はserviceに対する変更・apply・discardを排他制御する)
- Note: service の status が "active" でないとき(作成中の "pending"、"failed"、"deleting")は 409 を返す(ポートを変えられるのは、k8s の Service がある active のときだけ)。更新・適用・破棄の全てで同じ
- Note: 送った ports が、現在の ports と同じ内容(JSON の書式ではなく、中身で比べる)なら、pending_ports は保存せず null にする(以前の pending_ports も消える)。そのため、同じポートを apply し直すことはできない(409)
- Note: ports の検査は、作成のときと同じ(1〜20個、name の規則、port の範囲、name と port・protocol の重複は 400)
- Note: deploy の status が "deleting" の間は 409 を返す(deploy の削除に伴い service も deleting になるため)

### サービス 適用
- Post: /deploy/:deployId/service/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_ports の内容を ports に反映するため、k8s の Service のポートを更新する。**Pod は再起動しない**(Service は、Pod のポートに直接つなぐため。ポートの変更で、利用者のアプリは中断しない)。呼び出し自体は即座に応答するが、この時点ではまだ ports は更新されていない。pending_ports が null の場合は 409 を返す
- Note: 呼び出し直後、deploy の status は "applying" になる(他の操作を排他制御するため)。この間 service の pending_ports はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_ports の値を使って先に k8s の Service の更新を試み、その結果(成功・失敗)が確定した時点で、**成功なら** service の ports を pending_ports の内容に更新して pending_ports を null に戻し、deploy の status を "active" に戻す。**失敗なら**、k8s の Service を元の ports に戻した上で、ports も pending_ports も変えず(試して失敗した値は pending_ports に残る)、deploy の status を "failed" にする。ports は常に「いま動いている値」を表す。失敗のあとは、pending_ports を直して PATCH し直す、そのまま再度 apply する、discard で破棄する、のいずれもできる([デプロイ詳細取得](deploys.md#デプロイ詳細取得) でポーリングして確認する)
- Note: 既に deploy の status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)
- Note: apply 中に変化するのは deploy の status だけで、service の status("pending" | "active" | "failed" | "deleting")は変わらない。service の status は Service 自体の払い出し状態である。apply の進行は deploy の status が表す
- Note: deploy の replicas が 0(status が "scaled_zero")のときも、k8s の Service は更新でき、成功しても status は "active" ではなく "scaled_zero" に戻る。
- Note: deploy の status が "deleting" の間は 409 を返す(deploy の削除に伴い service も deleting になるため)
- Note: k8s の Service の更新は、一時的な障害なら、2分までリトライする(仕様の applying の 300秒に収まる)。それでも更新できなければ、上の失敗として扱う

### サービス 破棄
- Post: /deploy/:deployId/service/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_ports の内容を適用せずに破棄する(ports には影響しない)。pending_ports が null の場合は 409 を返す
- Note: deploy の status が "applying" の間は 409 を返す
- Note: deploy の status が "deleting" の間は 409 を返す(deploy の削除に伴い service も deleting になるため)

### サービス削除
- Delete: /deploy/:deployId/service
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: 作成中("pending")の service は、作成の処理が status を決めるので、削除できず 409 を返す。作成または削除に失敗した("failed")service は、もう一度削除できる(削除の処理は、15分までリトライし、超えると "failed" になる)
- Note: 呼び出すと即座に応答し、service の status は直ちに "deleting" になる。実体(k8s Service等)の削除処理は非同期に開始され、完了するとレコード自体が削除される([サービス取得](#サービス取得)は 404 を返すようになる)
- Note: 当該 service の各ポートを参照している ingress の path rule は、呼び出し時点(status: "deleting" になった時点)で連鎖して削除される
- Note: 既に status が "deleting" の間に再度削除を呼び出した場合は 409 を返す(削除処理中の多重実行不可)
- Note: deploy が削除されるときは、その service も連鎖して削除される(deploy が削除中の間、service の status は "deleting" として返る。k8s の Service は deploy の削除の Workflow が消し、DB の service の行は、deploy の行と一緒に消える)。この連鎖削除でも、上記の ingress の path rule の削除は同様に行われる
- Note: deploy の status が "deleting" の間にこの操作を呼び出した場合も 409 を返す

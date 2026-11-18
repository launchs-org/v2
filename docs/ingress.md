# ingress(Traefik の IngressRoute)

プロジェクトの外向きの入り口。`/projects/:projectId/ingresses` の API で作り、ルールを設定すると、Traefik の IngressRoute として k8s に作られる。API の仕様は [endpoints/ingresses.md](endpoints/ingresses.md)、呼び出しの流れは [call-flow.md](call-flow.md) の 5.7。

## ホスト名の払い出し

- 形: `<形容詞>-<色>-<動物>.<ベースドメイン>`(例: `brave-black-horse.shconn.org`)。単語は、暗号論的な乱数で選ぶ。重なったときは、作り直す。
- ベースドメイン: 環境変数 `INGRESS_BASE_DOMAIN`(既定 `shconn.org`)。
- 払い出さない名前: 環境変数 `INGRESS_RESERVED_SUBDOMAINS`(カンマ区切り。指定すると既定の一覧を置き換える。既定は `www,api,app,auth,admin,registry,logs,mail,static,docs,status,traefik,dashboard`)。名前そのもの、または、3つの単語のどれかが含まれると、払い出されない(大文字小文字は区別しない)。
- 設定は backend(払い出す側)だけが読む。ホスト名は、ingress の行に保存される。

## Traefik とクラスターの用意(利用者がすること)

このシステムは、Traefik までのリクエストの到達、ドメイン(DNS)、TLS を管理しない。

- Traefik をクラスターに入れる(IngressRoute の CRD を含む)。`TRAEFIK_API_VERSION`(既定 `traefik.io/v1alpha1`)と `TRAEFIK_ENTRY_POINTS`(既定 `web`)は、worker の環境変数。
- Traefik の namespace を、`INGRESS_NAMESPACE`(worker)に指定する。プロジェクトの NetworkPolicy が、その namespace からの通信を許可する([network-isolation.md](network-isolation.md))。指定しないと、Traefik から Pod へ届かない。
- 本番の RBAC では、worker に、`traefik.io` の `ingressroutes` の get / create / update / delete を許す。
- ベースドメインのワイルドカード(`*.shconn.org`)を、Traefik へ向ける(DNS)。

## 開発用のクラスター(Talos)での確認

Helm で Traefik を NodePort(80 → 30080)で入れて確認した(`traefik/traefik` 42.3.2)。

```
helm install traefik traefik/traefik --version 42.3.2 --namespace traefik --create-namespace \
  --set service.spec.type=NodePort --set ports.web.nodePort=30080 --set ports.websecure.expose.default=false \
  --set-string deployment.podLabels."admission\.datadoghq\.com/enabled"=false --set providers.kubernetesIngress.enabled=false
INGRESS_NAMESPACE=traefik task up:k8s
curl -H "Host: <払い出されたホスト名>" http://192.168.100.12:30080/
```

## 既知の問題: HTTPS を手前で終端すると、WebSocket が切れるアプリがある(code-server など)

状態: **原因は特定済み。修正は、別の修正として実装する(下の「修正の方針」)。** 2026-11-18 に、Talos で確認した。

### 症状
Cloudflare(Tunnel)で HTTPS を終端し、Traefik の `web`(HTTP)に入れている構成で、**code-server** を開くと、ログイン画面は出るが、ワークベンチに入ったあと、次のエラーが出る。

> The workbench failed to connect to the server (Error: WebSocket close with status code 1006)

ブラウザの WebSocket が、開いては閉じるを繰り返す。他のアプリ(エコーサーバー)の `wss://` は、同じ経路で、問題なく通る。

### 原因
Traefik の entryPoint は、**信頼していない相手(Cloudflare)から来た `X-Forwarded-*` を捨てて、自分の値に置き換える**(`forwardedHeaders.trustedIPs` を指定しない既定の動き)。そのため、アプリには、次のように届く(エコーサーバーで確認)。

| ヘッダー | アプリに届く値 | 本当の値 |
|---|---|---|
| `X-Forwarded-Proto` | `http` | `https`(本当の値は、`Cf-Visitor: {"scheme":"https"}` にだけ残る) |
| `X-Forwarded-Port` | `80` | `443` |
| `X-Forwarded-Host` | ホスト名 | 同じ |

code-server(4.145 で確認)は、WebSocket の握手のとき、`Origin`(ブラウザが付ける `https://<host>`。ポートは 443)と、`X-Forwarded-Host` + `X-Forwarded-Port` から組み立てた、リクエストの宛先を比べる。**`X-Forwarded-Port: 80` のせいで食い違い、握手を 403 で拒否する**(認証は、通っていても)。ブラウザには、`1006`(異常終了)として見える。

### 確認した内容(切り分け)
- **IngressRoute は原因ではない**: こちらが作る IngressRoute は、`Host(...) && PathPrefix(...)` → Service だけの素の設定。エコーサーバーの WebSocket は、Traefik 直接でも、Cloudflare 経由の `wss://` でも、双方向に通った(101 と、エコー)。
- **code-server への握手(認証つき、Origin は https)**: Traefik 経由は **403**。Pod に直接(`X-Forwarded-*` なし)なら **101**。
- **ヘッダーの絞り込み**(Pod に直接、1 つずつ足した): `X-Forwarded-Host` 単独、`X-Forwarded-Port: 80` 単独は 101。**`X-Forwarded-Host` と `X-Forwarded-Port: 80` の組み合わせが 403**(`X-Forwarded-Proto` は、無関係)。`X-Forwarded-Port: 443` か、ポート無しなら 101。`Origin` を `http://` にすると、101(ポート 80 と一致するため)。
- **直し方の検証**: Traefik の Middleware(`headers.customRequestHeaders`)で `X-Forwarded-Proto: https` と `X-Forwarded-Port: 443` を付けると、Cloudflare 経由の握手が 101 になり、**ブラウザで、エラーが消え、WebSocket が開いたままになった**(Middleware なしでは、エラーを再現した)。

### 再現の手順
1. エコーサーバー(`jmalloc/echo-server`)を、deploy + service + ingress で公開し、`curl https://<host>/` の応答で、アプリに届くヘッダーを見る(`X-Forwarded-Port: 80` になっている)。
2. code-server のテンプレートを作り、ログインして cookie を取る。`curl --http1.1 -H "Cookie: ..." -H "Origin: https://<host>" -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: ..." "https://<host>/?reconnectionToken=...&reconnection=false&skipWebSocketFrames=false"` で、**403** を確認する。Pod に `kubectl port-forward` して、`X-Forwarded-Host` と `X-Forwarded-Port` を足して、101 と 403 の境目を見る。
3. curl は、`--http1.1` が要る(HTTP/2 では、`Upgrade` ヘッダーが無効になる)。

### 修正の方針(別の修正として実装する)
運用側で直す方法も、ある。プラットフォームで直す方法を、実装する予定。

| 案 | 内容 | 向き不向き |
|---|---|---|
| **A. Middleware で、ヘッダーを上書きする(実装する)** | IngressRoute の route に、`X-Forwarded-Proto: https` と `X-Forwarded-Port: 443` を付ける Middleware を指定する | 手前で HTTPS を終端する構成向き(嘘のヘッダーになるので、HTTP のまま、直接使う構成には、向かない)。**検証済み** |
| B. Cloudflare Tunnel の向き先を、Traefik の `websecure`(https)にする | `TRAEFIK_ENTRY_POINTS=web,websecure` にして、cloudflared の向き先を `https://traefik`(証明書の検証はオフ)にする。Traefik が、正しい値(`443`、`https`)を付ける | コードの変更は要らない。運用側の設定変更 |
| C. Traefik の `forwardedHeaders.trustedIPs` に、cloudflared の Pod の範囲を指定する | Cloudflare が付けた `X-Forwarded-Proto: https` を、そのまま渡す | Cloudflare は `X-Forwarded-Port` を付けないので、これだけでは**直らない可能性が高い**(未検証) |

案 A の実装の案:
- 環境変数(worker): 上流が HTTPS を終端していることを示す(例: `INGRESS_FORWARDED_HTTPS=true`。既定は、無効)。
- 有効なとき、ingress ごとに、**Middleware**(`traefik.io` の `Middleware`、同じ namespace)を作り、IngressRoute の route の `middlewares` から参照する。ingress の削除で、Middleware も消す。
- 本番の RBAC: worker に、`traefik.io` の `middlewares` の get / create / update / delete を足す。
- テスト: worker のオペレーター(fake の dynamic クライアント)で、Middleware と route の参照を確認する。実機(Talos + Cloudflare)で、code-server の WebSocket が通ることを確認する。
- 決めること: Middleware の名前(ingress の ID から決める案)、既存の ingress への反映(次の「ルールの反映」で作り直す案)、無効に戻したときの扱い。

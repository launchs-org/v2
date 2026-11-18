# Traefik プラグイン: 本当のクライアント IP と、アクセスの集計

状態: **設計(実装の前)**。ingress の WebSocket の問題([ingress.md](ingress.md) の「既知の問題」)の修正と、アクセスの集計を、Traefik の**自作のプラグイン 2 つ**で行う。

## 1. 目的
1. **アプリまで、本当のクライアント IP が届く**。いまは、cloudflared の Pod の IP が届く(本当の IP は `Cf-Connecting-Ip` にだけ残る)。
2. **WebSocket が切れる問題の修正**。`X-Forwarded-Port: 80` と `Origin`(https)の食い違いを、正しい `X-Forwarded-Proto` / `X-Forwarded-Port` にして、直す。
3. **アクセスの集計**。ingress ごとに、リクエスト数、ステータスの内訳、メソッド、パス、クライアント IP、バイト数、応答時間を、集計して、画面で見られるようにする。**既定で有効**(ingress ごとに、無効にできる)。

## 2. 全体の構成
```
ブラウザ → Cloudflare(HTTPS を終端)→ cloudflared(Pod)→ Traefik の entryPoint「web」
              [launchs-edge]  ← entryPoint 全体に 1 つ(全てのルートに、自動で効く)
            → IngressRoute のルート
              [launchs-stats] ← ingress ごとの Middleware(プラットフォームが作る)
            → Service → アプリの Pod

launchs-stats ──(1 分ごとに、標準出力へ 1 行)──▶ Traefik の Pod のログ
accessstats(新しいコンテナ)──(k8s の API で、ログを読む)──▶ DB(集計)──▶ backend の API ──▶ 画面
```
- どちらのプラグインも、**Yaegi**(Go のソースを、Traefik が起動時に解釈して実行する)で書く。標準ライブラリだけを使う(外部パッケージは、使えない)。通常の `go test` で、試験できる。
- プラグインは、Traefik の**静的な設定**なので、追加・変更には、Traefik の再起動(Helm の upgrade)が要る。

## 3. launchs-edge(entryPoint 全体)
全リクエストが通るので、**小さく、安全に**保つ。

- **設定**: 信頼する接続元の範囲(`trustedCIDRs`。cloudflared の Pod の範囲)。
- **接続元が、信頼する範囲のとき**:
  - クライアント IP = `Cf-Connecting-Ip`(無ければ、`X-Forwarded-For` の先頭)。有効な IP のときだけ使う。
  - `X-Real-Ip` と、`X-Forwarded-For` の先頭に、その IP を入れる(元の `X-Forwarded-For` の続きは、残す)。
  - `X-Forwarded-Proto` = `Cf-Visitor` のスキーム(無ければ、元の値)。`X-Forwarded-Port` = https なら 443、http なら 80。
- **接続元が、信頼する範囲の外のとき**(LAN から NodePort へ直接など): `Cf-Connecting-Ip`、`Cf-Visitor`、`True-Client-Ip` を**捨てる**(なりすまし対策)。`X-Real-Ip` は、接続元の IP にする。
- パニックしても、リクエストを止めない(復帰して、そのまま通す)。

## 4. launchs-stats(ingress ごと)
その ingress の Middleware として入る。リクエストを、メモリの中で、集計する。

### 設定(Middleware の `spec.plugin`)
`ingressId`(必須)、`intervalSeconds`(既定 60)、`clientIPHeader`(既定 `X-Real-Ip`。launchs-edge が整えた値を読む)、上限(下の「上限」)。

### 集計する項目(1 区間 = 60 秒、ingress ごと)
| 項目 | 内容 |
|---|---|
| `count` | リクエスト数 |
| `status` | ステータスの種類ごとの数(`1xx` `2xx` `3xx` `4xx` `5xx`) |
| `methods` | メソッドごとの数(`GET` `POST` など。知らないメソッドは `OTHER`) |
| `bytes` | 応答で送ったバイト数の合計 |
| `latency` | 応答時間のヒストグラム(境界 ms: 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, それ以上)と、合計(平均を出すため) |
| `paths` | **パスの上位 50 件**(件数、バイト数、`5xx` の数) |
| `clients` | **クライアント IP の上位 50 件**(件数、バイト数)。**IP の種類の数**(`uniqueClients`。1 区間で、最大 10000 まで数え、それ以上は 10000 + とする) |

### パスの正規化(種類が、無限に増えるのを防ぐ)
- クエリ(`?` 以降)は、捨てる。長さは、200 文字まで(超えたら、切る)。
- 区間ごとの、数字だけのセグメント、UUID、16 文字以上の 16 進数は、`:id` に置き換える(`/users/123/posts` → `/users/:id/posts`)。
- **上位 50 件に入らなかったものは、`(other)` に合算**する。

### 上限(メモリが、攻撃で膨らまない)
- パスと IP の上位は、**Space-Saving 法**(固定の個数のカウンターで、上位を、近似して数える)。1 区間、1 ingress につき、**100 個のカウンター**まで。出力は、上位 50 件。
- 種類の数のための IP の集合は、10000 件まで。
- 1 ingress につき、使うメモリは、数百 KB で、頭打ちになる。

### 出力
- **1 区間ごとに、1 ingress につき、1 行**を、標準出力に出す(リクエストが無い区間は、出さない)。区間の境界は、時刻に揃える(`start` は、区間の開始の Unix 時刻)。
- 行: `LAUNCHS_STATS {"v":1,"ingress":"ingress-…","replica":"<Traefik の Pod 名>","start":1700000000,"seconds":60,"count":…,"status":{…},"methods":{…},"bytes":…,"latency":{"buckets":[…],"sumMs":…},"paths":[{"k":"/","n":…,"b":…,"e":…}],"clients":[{"k":"1.2.3.4","n":…,"b":…}],"uniqueClients":…}`
- 区間の終わりごとに出す goroutine は、**ingress ID ごとに 1 つだけ**(プラグインのパッケージの中の表で共有)。Traefik の設定の再読み込みで、Middleware が作り直されても、増えない。
- 応答を包むので、`Hijack`(WebSocket の切り替え)、`Flush`、`ReadFrom` を、そのまま通す。**WebSocket が壊れないことを、必ず、検証する。**

### クライアント IP の信頼
`clientIPHeader` の値を読む。launchs-edge が入っていない構成では、接続元の IP(cloudflared の Pod)になる(害は無いが、役に立たない)。

## 5. 収集(accessstats。新しいコンテナ)
- `logcollector` と同じ作り(リーダー選出は advisory lock。k8s の API でログを読む)。対象は、**Traefik の Pod のログ**(namespace と、ラベル `app.kubernetes.io/name=traefik`。環境変数で指定)。`LAUNCHS_STATS ` で始まる行だけを読む。
- 行を読み込んで、**1 分の行**(`ingress_access_minutes`)に、上書きで保存する。キーは、`(ingress_id, replica, start)`。**同じ行を、重複して読んでも、結果は、変わらない**。
- ingress の ID が、DB に無い行は、捨てる(削除された ingress)。
- 大きさが、桁違いの行(1 MiB 超)や、壊れた JSON は、捨てて、数を数える(ログに出す)。
- 読み始める位置は、前回の続き(`sinceTime`)。再起動で、重複して読むのは、よい(上書きで、安全)。

### データ
- `ingress_access_minutes`: `ingress_id`、`replica`、`start`(時刻)、`seconds`、`count`、`status`(jsonb)、`methods`(jsonb)、`bytes`、`latency`(jsonb)、`paths`(jsonb。上位 20 件に縮める)、`clients`(jsonb。上位 20 件に縮める)、`unique_clients`。**保存は 24 時間**。
- `ingress_access_hours`: `ingress_id`、`hour`(時刻)。上の合計と、`paths`・`clients` の**上位 100 件**を、Pod をまたいで、合算した値。**保存は 30 日**。1 時間の区切りが終わった分を、まとめる(collector が、定期的に行う)。
- クライアント IP は、個人情報になりうる。保存期間は、上のとおり**最大 30 日**。IP の保存を止めたいときは、環境変数(`ACCESS_STATS_KEEP_CLIENTS=false`)で、収集のときに、捨てられる。

## 6. API(backend。読むだけ)
- `GET /projects/:projectId/ingresses/:ingressId/access-stats?range=1h|24h|7d|30d`
  - `range` が `1h`、`24h` のときは「1 分の行」から、`7d`、`30d` は「1 時間の行」から作る。
  - レスポンス: `{ "range", "intervalSeconds", "enabled", "series": [{ "t", "count", "status": {…}, "bytes", "latencyAvgMs", "latencyP95Ms" }], "totals": { "count", "status", "methods", "bytes", "latencyAvgMs", "latencyP95Ms", "uniqueClients" }, "topPaths": [{ "path", "count", "bytes", "errors" }], "topClients": [{ "ip", "count", "bytes" }] }`
  - 応答時間の P95 は、ヒストグラムから、近似して出す。
- ingress の詳細に、`accessStats`(boolean。**既定 true**)を足す。`PATCH /projects/:projectId/ingresses/:ingressId`(新設。`{ "accessStats": false }`)で切り替える。切り替えたら、IngressRoute を、反映し直す。

## 7. プラットフォーム(worker)
- 環境変数 `INGRESS_STATS_PLUGIN`(プラグインの名前。例 `launchs-stats`)。**空のときは、機能が無効**(Middleware を作らず、ルートも変えない)。**プラグインが入っていない Traefik に、Middleware を参照するルートを渡すと、そのルートが壊れる**ため、運用者が、プラグインを入れてから、指定する。
- 有効で、ingress の `accessStats` が true のとき、ingress の反映で、同じ namespace に **Middleware**(`traefik.io`。名前は ingress の ID から決める)を作り、IngressRoute の route の `middlewares` から参照する。無効のときは、Middleware と参照を、外す。ingress の削除で、Middleware も消す。
- 本番の RBAC: worker に、`traefik.io` の `middlewares` の get / create / update / delete を足す。accessstats に、Traefik の namespace の `pods` の get / list / watch と `pods/log` の get を許す。
- **既存の ingress に、自動では、Middleware を足さない**(プラグインが無い Traefik で、既存のルートを壊さないため)。有効にして、次に、その ingress を反映したとき(または、切り替えたとき)に足す。

## 8. 導入(運用者がすること)
1. プラグインのソースを、Traefik に渡す(Helm の `experimental.localPlugins` の `inlinePlugin`)。values の例を、`traefik-plugins/helm-values.example.yaml` に置く。
2. `ports.web.http.middlewares` に、launchs-edge の Middleware を指定する(Traefik の namespace に、Middleware を 1 つ作る)。`trustedCIDRs` は、cloudflared の Pod の範囲。
3. Traefik を再起動する(Helm の upgrade)。
4. worker の環境変数 `INGRESS_STATS_PLUGIN` を指定し、accessstats を起動する。

## 9. リスクと対策
| リスク | 対策 |
|---|---|
| プラグインのバグが、全体のアクセスに影響する | 小さく保つ。パニックから復帰して、通す。十分なテスト。Yaegi 上での実行を、テストで確認する |
| WebSocket が壊れる | `Hijack` などを通す。WebSocket のテストを、必須にする |
| 攻撃で、パスや IP の種類が増え、メモリが膨らむ | Space-Saving 法で、固定の個数。正規化。1 ingress につき、頭打ち |
| 設定の再読み込みで、goroutine が増える | ingress ID ごとに 1 つ(パッケージの表で共有) |
| IP が、なりすまされる | 信頼する範囲の外からの `Cf-*` を捨てる |
| プラグインの無い Traefik で、ルートが壊れる | `INGRESS_STATS_PLUGIN` が空なら、何もしない。既存の ingress を、自動では、変えない |
| Traefik の再起動で、最大 1 区間(1 分)の集計が、失われる | 許容する |

## 10. 検証の計画
1. プラグインを、Yaegi で読み込んで、httptest で試験する(`traefik-plugins/harness`)。ヘッダーの整え方、集計、上限、WebSocket の通過。
2. **本番の Traefik には触らず**、私の専用の namespace に、別の Traefik を、プラグイン付きで立てて、実際の通信で確認する(エコーサーバー、WebSocket、code-server)。
3. プラットフォーム(worker、accessstats、API、画面)を、その Traefik で、通しで確認する。
4. 本番の Traefik への適用は、**あなたの判断**で行う(手順を、ドキュメントに書く)。

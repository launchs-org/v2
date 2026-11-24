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
1. launchs-edge の Middleware を、Traefik の namespace に作る(`traefik-plugins/edge-middleware.example.yaml`。`trustedCIDRs` は、cloudflared の Pod の範囲)。**先に作る**(Traefik が起動するとき、entryPoint が参照する Middleware が、必要なため)。
2. `python3 traefik-plugins/render_helm_values.py > plugins-values.json` で、Helm の values(`experimental.localPlugins` の `inlinePlugin` と、`ports.web.http.middlewares`)を作る。今の values と重ねて、`helm upgrade traefik traefik/traefik --version <今の版> -n traefik -f <今の values> -f plugins-values.json --atomic` を行う(chart の ConfigMap にソースが入り、Traefik が再起動する。失敗したら、自動で戻る)。ソースを変えたときも、同じ手順。
3. `helm template` の差分で、変わるのが、ConfigMap(2 つ)と、Deployment の引数・マウントだけであることを、先に確かめる。
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

## 11. 実装と確認の結果(2026-11-18)
実装は、設計どおり。違いと、確認で分かったことを書く。

### 設計との違い(実装が正)
- **出力の行**: `LAUNCHS_STATS {"ingressId","start"(RFC3339),"intervalSec","count","status":{"1xx":…,"5xx":…},"methods","bytes","latencyBounds","latency"(度数。最後は「それ以上」),"sumMs","paths":[{"key","count"}],"clients":[{"key","count"}],"uniqueClients","uniqueCapped"}`。パスごとのバイト数とエラー数は、持たない(件数だけ)。
- **WebSocket とイベントストリームは、応答を包まない**。Yaegi は、interpreter の中で包んだ `ResponseWriter` の `Flush` を、下流に見せない(`http.Flusher` の確認が失敗する。`Hijack` は通る)。`Upgrade` ヘッダーのあるリクエストと、`Accept: text/event-stream` のリクエストは、素通しにして、接続の開始だけを数える(ステータスは、記録しない)。通常のリクエストの `Flush` は、包んだ応答では、使えない(`Unwrap` を足しても、Yaegi の包みでは、`ResponseController` からも見えない)。
- **API のレスポンス**: `dev-docs/endpoints/ingresses.md` の「アクセスの集計の取得」が正。`topPaths` ではなく `paths`、`series` の各点に、メソッドと分位も入る。
- **テーブル**: `bucket_start`(時刻)、`interval_sec`。`ingress_access_hours` は、`replica` が空の行(Pod をまたいで合算)。ingress の削除では、集計の行を消さない(保存期間で消える)。
- accessstats は、Pod の一覧から、`Running` の Traefik の Pod を 10 秒ごとに見て、Pod ごとに、ログを追う(最初は 300 秒さかのぼる)。切れたら、3 秒後に、120 秒さかのぼって、つなぎ直す。同じ行は、上書きされるだけ。1 分ごとに、1 時間への、まとめ直し(直近 2 時間)と、保存期間を過ぎた行の削除を行う。

### 実機での確認(Talos の、専用 namespace の Traefik v3.7.17。本番の Traefik には、触っていない)
- 2 つのプラグインを、ConfigMap から `/plugins-local/src/...` に置いて、`experimental.localPlugins` で読み込めた。launchs-edge は、`--entryPoints.web.http.middlewares=<ns>-<名前>@kubernetescrd` で、全てのリクエストに掛かった。
- **信頼された接続元(Pod の範囲)**から `Cf-Connecting-Ip`、`Cf-Visitor: {"scheme":"https"}` を付けると、アプリには `X-Real-Ip: <クライアント>`、`X-Forwarded-For: <クライアント>, <接続元>`、`X-Forwarded-Proto: https`、`X-Forwarded-Port: 443` が届いた(code-server の WebSocket が 403 になる原因の、`X-Forwarded-Port: 80` が直る)。**信頼されない接続元**(`kubectl port-forward` の 127.0.0.1)からの `Cf-Connecting-Ip` と `X-Forwarded-For` の偽装は、捨てられた。
- WebSocket のハンドシェイク(`/echo`)は、集計のプラグインを通して、101 になった。
- プラットフォームで作った ingress(deploy + service + ingress、ルールの反映)が、Middleware(`<ingress の ID>-stats`)と、それを参照する IngressRoute になり、通信が、集計された。accessstats が、DB に保存し、API が、件数、ステータス、メソッド、パス(UUID と数字は `:id`、クエリは除去)、クライアントの IP を返した。
- `PATCH` で `accessStats` を false にすると、Middleware が消えて、route の参照が外れ(通信は、200 のまま)、true に戻すと、再び付いた。
- 確認が終わったあと、検証用のプロジェクトと namespace を、全て削除した。
- 本番の Traefik への適用は、§12。

### 運用の注意
- 検証では、`task up:k8s` のあと、別の `docker compose run` で、環境変数(`INGRESS_STATS_PLUGIN`、`ACCESS_STATS_TRAEFIK_NAMESPACE`、`ACCESS_STATS_TRAEFIK_SELECTOR`)が、コンテナに残らないことがあった。`.env` に書く(`.env.example` に、例を置いた)か、毎回、指定する。
- 試験用の Traefik には、Traefik の namespace 以外の Pod が、プロジェクトの namespace に入るための、NetworkPolicy(`dev-docs/network-isolation.md`)の許可が要る。本番の Traefik は、`INGRESS_NAMESPACE` で、許可済み。
- プラグインが入っていない Traefik に、`INGRESS_STATS_PLUGIN` を指定すると、ルートが、壊れる。必ず、先に Traefik にプラグインを入れる。

## 12. 本番の Traefik への適用(2026-11-18)
- 手順は §8。chart `traefik-42.3.2`(アプリ v3.7.17)の release `traefik`(namespace `traefik`)を、revision 1 → 2 に upgrade した(`--atomic`)。`trustedCIDRs` は `10.240.0.0/16`(Pod の範囲。cloudflared は、この中にいる。プロジェクトの namespace は、NetworkPolicy で、Traefik 以外からの通信を受けないので、他の Pod が、Cf-* を偽装して、直接つなぐことはできない)。
- 適用の前後で、既存の 3 つの ingress の応答は、同じ(200、302、200)。Cloudflare 経由の公開 URL(`fancy-gold-zebra.shconn.org` は 302、`humble-amber-sparrow.shconn.org` は 200)も、同じ。Traefik のログに、プラグインの読み込みが出て、エラーは無い。
- **まだ**: worker の `INGRESS_STATS_PLUGIN=launchs-stats`(ingress への Middleware の付与)と、accessstats の本番での起動(`ACCESS_STATS_TRAEFIK_NAMESPACE=traefik`)。worker の RBAC に `middlewares` を足し、accessstats に、Traefik の namespace の `pods` の list と `pods/log` の get を許してから、行う。code-server の WebSocket(1006)が、直ったかの確認も、まだ。
- 戻すとき: `helm rollback traefik 1 -n traefik`(Middleware `launchs-edge` が残っても、無害)。

## 13. アクセスログ(1 リクエストごと。2026-11-18 に追加)
「アクセスの集計」だけでは、1 つずつのリクエストが見えないため、アクセスログを足した。
- **プラグイン**: 出力の間隔の既定を、60 秒から **10 秒**に縮め(worker が作る Middleware も 10 秒)、出力の行に、`requests`(`t` ミリ秒の時刻、`m` メソッド、`u` パスとクエリ、`s` ステータス、`d` ミリ秒、`ip`、`b` バイト)を足した。**クエリ文字列も、落とさない**。1 回の出力に入れるのは、最大 200 件(`requestsDropped` に、超えた数)。URL は 1000 バイトまで。メモリは、上限で頭打ち。
- **保存**: `ingress_access_logs`(1 リクエストにつき 1 行)。`(ingress_id, replica, batch_start, seq)` が一意で、同じ出力を読み直しても、二重にならない。accessstats が、保存期間(`ACCESS_LOG_RETENTION`。既定 24 時間)と、ingress ごとの行数(`ACCESS_LOG_MAX_ROWS`。既定 5000)で、1 分ごとに削除する。集計の行(`ingress_access_minutes`)は、出力の間隔が短くなったので、行が増える(区間の始まりの時刻は、そろえていない。API が、1 分ごとなどに足し合わせる)。
- **API**: `GET .../ingresses/:ingressId/access-logs`(`dev-docs/endpoints/ingresses.md`)。
- **画面**: ingress パネルの「アクセスログ」は、**表だけ**(グラフは無し)。列は、日時、メソッド、パス(URL)、ステータス、時間、IP、サイズ。日時の範囲、パス(URL に含まれる文字)、IP、ステータス、メソッドで絞り込める。「更新」ボタンと、自動更新(5 秒。切り替えられる)、「記録する」(有効・無効)、「さらに読み込む」(古い分)。集計の API(`access-stats`)は、画面からは使わなくなったが、残している。
- **取りこぼし**: 反映(Middleware が付く)の前のアクセスは、記録されない。WebSocket などは、接続の開始だけ(ステータス 0)。
- **個人情報**: URL(クエリを含む)と IP を、保存期間の間、保存する。クエリに秘密が入るアプリは、ログに残る。IP だけ止めるなら `ACCESS_STATS_KEEP_CLIENTS=false`。


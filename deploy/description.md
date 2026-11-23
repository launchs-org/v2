# 構成と、クラスター側の前提

launchs の本番向けのマニフェスト(`deploy/`)の、中身と決まり。入れ方と更新は [deploy.md](deploy.md)、運用は [operation.md](operation.md)、つまずいたときは [questions.md](questions.md)。

## 構成(本体と設定は、別に適用する)
| 場所 | 内容 | 適用 |
|---|---|---|
| `deploy/`(この場所) | 本体(PostgreSQL、Temporal、auth、backend、worker、…、IngressRoute) | `kubectl apply -k .` |
| `deploy/settings/` | **環境ごとの設定**: ConfigMap `launchs-config`(`config.yaml`)と、Secret(`secret.yaml`。gitignore) | `kubectl apply -k settings/` |

設定は、本体より**先に**適用する(各 Deployment が参照するため)。本体の `kubectl apply -k .` は、設定を**変えも、消しもしない**。設定を変えたいときは、`settings/` だけを、適用し直す(本体の更新と、切り離せる)。`secret.example.yaml`(雛形)は、`settings/kustomization.yaml` に含めていないので、誤って適用されない。

## 構成と権限
| 名前 | 内容 | k8s への権限(rbac.yaml) |
|---|---|---|
| postgres | StatefulSet 1 つ。app、auth、temporal、temporal_visibility の 4 つの DB | なし |
| temporal | 単一ノード。起動前に、スキーマを作る | なし |
| auth | 認証と JWKS。署名鍵は Secret を `/keys` にマウント | なし |
| backend | API。`AUTH_REQUIRE_JWKS=true`(開発用の `DEV_USER_ID` を拒否) | なし |
| worker | プロジェクトの namespace、deploy、service、ingress などを作る・消す | namespace、Deployment、Service、ConfigMap、PVC、NetworkPolicy、IngressRoute、Middleware などを操作 |
| watcher | Pod と Deployment を観測して、DB に書く(リーダーは 1 つ) | Pod、Deployment、イベントの読み取り |
| logcollector | deploy のログを DB に保存(リーダーは 1 つ) | Pod の読み取りと、ログの取得 |
| accessstats | Traefik のログから、アクセスログを DB に保存(リーダーは 1 つ) | **Traefik の namespace だけ**の Pod の読み取りと、ログの取得(Role) |
| frontend | Next.js(standalone) | なし |

watcher、logcollector、accessstats は、複数起動しても、advisory lock で、リーダーの 1 つだけが動く(残りは、待機で、リーダーが落ちたら、引き継ぐ)。

## 高可用性(3 つのノード)
- **アプリ(auth、backend、worker、watcher、logcollector、accessstats、frontend)と Temporal は、3 つずつ**動かす(`kustomization.yaml` の `replicas`。数を変えるのは、ここだけ)。`topologySpreadConstraints` で、3 つのノードに、ばらけさせる(ノードが足りないときは、偏ってもよい)。`pdb.yaml` の PodDisruptionBudget で、ノードの入れ替え(drain)のときも、最低 2 つを、動かし続ける。
- **Temporal**: 各 Pod が、全てのサービス(frontend、history、matching、worker)を動かし、DB の `cluster_membership` で見つけ合って、1 つのクラスターになる(`broadcastAddress` は、各 Pod の IP)。1 つずつ入れ替える(RollingUpdate)。**`numHistoryShards`(512)は、最初に起動するときに決まり、後から変えられない**。
- **PostgreSQL は、1 つのまま**。データは、Longhorn の PVC(既定の StorageClass は、3 複製)に置くので、ディスクやノードが 1 つ壊れても、データは残る。ただし、**PostgreSQL の Pod 自体は 1 つ**なので、そのノードが落ちると、別のノードで再起動するまで(数十秒〜数分)、DB が止まる。Pod を増やすだけでは、別々の DB になり、壊れる(複製の仕組みが要る)。ノードが落ちたとき、自動で別のノードに移るには、Longhorn の設定 `node-down-pod-deletion-policy` を、`delete-both-statefulset-and-deployment-pod` にする(既定の `do-nothing` では、Pod が Terminating のまま、手で消すまで、動かない)。より強い高可用性(自動フェイルオーバー)が要るなら、CloudNativePG などの、PostgreSQL のオペレーターに替える。
- **接続数**: PostgreSQL の `max_connections` は 300(`postgres.yaml`)。アプリは、1 プロセスあたり最大 10 接続(`shared/database`)、Temporal は、Pod ごとに約 30 接続で、3 つずつ動かすと、約 250 になる。レプリカを増やすときは、`max_connections` も見直す。

## 含まれないもの
- **バックアップ**: PostgreSQL のバックアップ(PVC のスナップショットや `pg_dump`)は、運用側で行う。
- **TLS と DNS**: Cloudflare などで行う(このマニフェストは、HTTP の `web` の entryPoint に出す)。
- **Traefik 自体**と、プラグインの導入は、この `kubectl apply -k .` には含まれない(別の Application `traefik`、`launchs-cluster`。`argocd/applications.yaml`。この文書の「クラスター側(土台)」)。
- **Temporal の認証**: Temporal は、クラスターの外に出さない前提で、認証を使わない(`TEMPORAL_ALLOW_NO_AUTH`)。Service(`temporal`)は ClusterIP のみ。

## 実行ユーザー
auth、backend、worker などは、`runAsUser: 65532`(distroless の nonroot)、frontend は、`runAsUser: 1000`(node)を、**数字で**指定している。イメージの `USER` は、名前(`nonroot`、`node`)で、kubelet は、名前の ID を確かめられず、`runAsNonRoot: true` だけでは、`container has runAsNonRoot and image has non-numeric user` で、起動できないため。

## 検証したこと
- `kubectl kustomize .` で描画でき、全てのリソースが、server-side dry-run を通る。
- PostgreSQL と Temporal は、使い捨ての namespace に、実際に立ち上げて確認した(スキーマの作成、`default` の namespace の作成、再起動して、スキーマ作成を、もう一度流しても壊れないこと)。
- **まだ**: launchs の各イメージ(backend、worker など)を使った、通しの確認(`main` への push で、イメージが GHCR に出てから)。

# クラスター側(土台)

`deploy/cluster/` と `deploy/argocd/` の説明。**Argo CD で、launchs の土台(Traefik とプラグイン、metrics-server、cloudflared)も、全部入る**(入れ方は [argocd.md](argocd.md))。

## 全部入れる(Argo CD)
Application は `deploy/argocd/applications.yaml` の 3 つ。同期は手動で、順序は `traefik` → `launchs-cluster` → `launchs-prod`(手順は [argocd.md](argocd.md))。

| Application | 内容 | 場所 |
|---|---|---|
| `traefik` | Traefik(Helm のチャート 42.3.2)と、プラグイン `launchs-edge`、`launchs-stats`、基本の設定 | チャート + `deploy/cluster/traefik-values.yaml` |
| `launchs-cluster` | metrics-server、launchs-edge の Middleware、cloudflared(DaemonSet)、`traefik` の namespace | `deploy/cluster/`(kustomize) |
| `launchs-prod` | launchs 本体 | `deploy/` |

- **プラグインのソース(`traefik-plugins/`)を変えたら**: `python3 traefik-plugins/render_helm_values.py --full > deploy/cluster/traefik-values.yaml` で作り直して、一緒にコミットする(CI が、ずれを検査する)。
- **クラスターごとに違う値**: `launchs-edge` の `trustedCIDRs`(cloudflared の Pod の範囲)は、Git では本番 launchs.org の `10.244.0.0/16`。別の範囲のクラスターは、`applications.yaml` の `launchs-cluster` の `kustomize.patches`(コメントの例)で上書きする。**合っていないと、Cf-* のヘッダーを信じず、クライアント IP と https が、アプリに届かない**。
- **既に Helm で入れた Traefik がある場合**: 同じ版・同じ values なら、描画は同じ(確認済み)。同期しても、中身は変わらず、Argo CD が管理を引き継ぐ(Helm の release の記録は、残る)。
- cloudflared のトークン(Secret `tunnel-token`)は、Git に入れない(雛形: `deploy/cluster/tunnel-token.example.yaml`)。

以下の「調べること」〜「Traefik のプラグイン」は、**Argo CD なしで、手で入れる場合**の記録と、参考の値。

## 調べること(推測しない)
```bash
kubectl get nodes -o wide
kubectl cluster-info dump | grep -m3 -E "cluster-cidr|service-cluster-ip-range"
kubectl get sc
kubectl get pods -A | grep -iE "traefik|cloudflared|metrics-server|datadog"
helm list -n traefik
```
本番の Talos の値: Pod `10.244.0.0/16`、Service `10.96.0.0/12`、ノード `192.168.10.161-165`、StorageClass `longhorn`(既定)と `local-path`、Traefik は chart 41.6.1(namespace `traefik`、entryPoint `web`=8000)、入口は Cloudflare Tunnel(cloudflared)。Datadog は無い。

## 環境ごとの設定(`deploy/settings/`)
`settings/config.launchs.org.example.yaml` が、本番の値の例(適用の対象ではない)。`config.yaml` に写して使う。

| キー | 本番の値 | 理由 |
|---|---|---|
| `INGRESS_BASE_DOMAIN` | `launchs.org` | 公開ドメイン。IngressRoute の Host(`ingressroute.yaml`)も `www.launchs.org` にする |
| `AUTH_TRUSTED_PROXY_CIDRS` | `10.244.0.0/16` | Traefik の Pod の範囲(広すぎると、クライアント IP を偽装される) |
| `VOLUME_STORAGE_CLASS` | `longhorn` | 利用者の volume(RWX)。Postgres の PVC も、既定の `longhorn` |
| `INGRESS_STATS_PLUGIN` | `launchs-stats` | §3 のプラグインを入れたあとだけ。**入れていない Traefik では、空にする** |

secret は `settings/secret.example.yaml` から作る(コミットしない)。`launchs-secret` の `POSTGRES_PASSWORD`(`openssl rand -hex 24`)、`API_TOKEN_SECRET`(`openssl rand -hex 64`)、最初の管理者、Turnstile のキー。`launchs-auth-keys` は `openssl genpkey -algorithm ed25519`。

## metrics-server(`metrics-server.yaml`)
無いと、画面のメトリクスの CPU とメモリが null になる。Talos の kubelet の証明書は自己署名なので、`--kubelet-insecure-tls` が要る(同梱のマニフェストに入っている)。
```bash
kubectl apply -f cluster/metrics-server.yaml
kubectl -n kube-system rollout status deploy/metrics-server
kubectl top nodes
```
適用の直後は、一部のノードが `<unknown>` になることがある(少し待つ)。

## Traefik のプラグイン(launchs-edge と launchs-stats)
- `launchs-edge`: Cloudflare 経由で、元のスキーム(https)とクライアント IP を、Traefik が受け取れない問題を直す(`Cf-Connecting-Ip`、`Cf-Visitor` から `X-Real-Ip`、`X-Forwarded-Proto/Port` を作る)。**入れないと、code-server などの WebSocket が 1006 で切れる**。
- `launchs-stats`: アクセスの集計とアクセスログ(accessstats が、Traefik のログから読む)。
- 設計は `docs/ingress-plugins.md`、ソースは `traefik-plugins/`。**Traefik の再起動を伴う**(1 レプリカだと、数秒、全ての公開が止まる)。

```bash
# 1) Middleware を先に作る(trustedCIDRs は cloudflared の Pod の範囲 = Pod の CIDR)
kubectl apply -f cluster/traefik-edge-middleware.yaml
# 2) 今の values を控え、プラグインの values を作る
helm get values traefik -n traefik -o yaml > current-values.yaml
python3 traefik-plugins/render_helm_values.py > plugins-values.json
# 3) 差分の確認(ConfigMap 2 つ、起動引数、マウントだけのはず)
helm get manifest traefik -n traefik > before.yaml
helm template traefik traefik/traefik --version <今の chart の版> -n traefik -f current-values.yaml -f plugins-values.json > after.yaml
diff before.yaml after.yaml
# 4) 適用(失敗したら、自動で戻る)
helm upgrade traefik traefik/traefik --version <今の chart の版> -n traefik -f current-values.yaml -f plugins-values.json --atomic --timeout 5m
```
確認: `kubectl -n traefik logs deploy/traefik | grep -i plugins` に、`launchs-edge` と `launchs-stats` の読み込みが出る。戻すとき: `helm rollback traefik <前の revision> -n traefik`。
**既存の ingress には、Middleware が自動では付かない**。次に反映したとき(または、切り替えたとき)に付く。

## Cloudflare(ダッシュボード。手作業)
- Tunnel の公開ホスト名に、`www.launchs.org` と `*.launchs.org` を、Traefik の HTTP(`http://traefik.traefik:80`)で向ける。
- **Zero Trust の Access のアプリケーションは、`www.launchs.org` と `*.launchs.org` に付けない**(付けると、ログイン画面に飛ばされ、利用者もアプリも開けない)。
- **Bot Fight Mode は、オフ**(オンだと、GitHub Actions の `POST /hooks/deploy` が Managed Challenge で止まる。Free プランでは、スキップできない)。
- Turnstile を使うなら、ウィジェットの許可ホスト名に `www.launchs.org` を入れる。

## 既存の古いものの扱い
旧 launchs(namespace `launchs-org`)は、DB の PVC が `local-path`(削除するとデータが消える)なので、**IngressRoute だけ**を外し、あとは残した。残したものを消すときは、先にデータを移す。

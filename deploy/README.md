# 本番向けのマニフェスト(kustomize)

launchs 自身を、Kubernetes に置くためのマニフェスト。**環境ごとの設定(`settings/`。ConfigMap と Secret)を先に適用してから**、この場所で `kubectl apply -k .` すると、PostgreSQL、Temporal、認証、API、ワーカー、watcher、logcollector、accessstats、フロントエンド、公開(Traefik の IngressRoute)が、namespace `launchs` に立ち上がる。

イメージは、`main` に push されるたびに、GitHub Actions(`.github/workflows/release.yml`。先に `test.yml` が通る)が、GHCR(`ghcr.io/launchs-org/v2/<名前>`)に push する。タグは `sha-<コミットの短い SHA>` と `latest`。

## 前提
- Traefik が入っている(CRD の `traefik.io` を含む。プラグインを使うなら [docs/ingress-plugins.md](../docs/ingress-plugins.md) の手順で入れる。入れていないなら、`settings/config.yaml` の `INGRESS_STATS_PLUGIN` を空にする)。
- 既定の StorageClass があり、PVC を作れる(PostgreSQL が 20Gi を使う)。
- Traefik の前段(Cloudflare Tunnel など)で、HTTPS を終端する(このマニフェストは、Traefik の `web` の entryPoint に出す)。
- GHCR のイメージを、クラスターが取得できる(パッケージが非公開なら、`imagePullSecrets` を、各 Deployment に足す)。

## 構成(本体と設定は、別に適用する)
| 場所 | 内容 | 適用 |
|---|---|---|
| `deploy/`(この場所) | 本体(PostgreSQL、Temporal、auth、backend、worker、…、IngressRoute) | `kubectl apply -k .` |
| `deploy/settings/` | **環境ごとの設定**: ConfigMap `launchs-config`(`config.yaml`)と、Secret(`secret.yaml`。gitignore) | `kubectl apply -k settings/` |

設定は、本体より**先に**適用する(各 Deployment が参照するため)。本体の `kubectl apply -k .` は、設定を**変えも、消しもしない**。設定を変えたいときは、`settings/` だけを、適用し直す(本体の更新と、切り離せる)。`secret.example.yaml`(雛形)は、`settings/kustomization.yaml` に含めていないので、誤って適用されない。

## 手順
0. **namespace を作る**(設定が、namespace `launchs` に入るため):
   ```sh
   kubectl apply -f namespace.yaml
   ```
1. **秘密を作る**(`settings/` で):
   ```sh
   cd settings
   cp secret.example.yaml secret.yaml
   openssl rand -hex 24                       # → POSTGRES_PASSWORD(英数字だけにする)
   openssl genpkey -algorithm ed25519         # → launchs-auth-keys の key-1.pem に貼る
   ```
   `secret.yaml` に、`POSTGRES_PASSWORD`、最初の管理者の名前とパスワード、署名鍵、`API_TOKEN_SECRET`(API トークンの署名の秘密。`openssl rand -hex 64`。空だと API トークンは使えない)(必要なら Turnstile のキー)を書く。`secret.yaml` は gitignore 済み(リポジトリには置かない)。
2. **設定を直す**: `settings/config.yaml`(ingress に払い出すホスト名のベースドメイン `INGRESS_BASE_DOMAIN` など)と、本体の `ingressroute.yaml`(`launchs.example.com` を、自分のホスト名に。3 か所)。
3. **(任意)イメージのタグを固定する**: `kustomization.yaml` の `images` の `newTag` を、`latest` から `sha-xxxxxxx` に変える。`main` に入れたあとは、CI が、`newTag` を、自動で、最新の `sha-…` に書き換える([OPERATIONS.md](OPERATIONS.md) の「タグの自動更新」)。
4. **適用する**(設定 → 本体の順):
   ```sh
   kubectl apply -k settings/       # 設定(ConfigMap と Secret)
   kubectl apply -k .               # 本体
   kubectl -n launchs get pods -w
   ```
   backend と auth の initContainer が、マイグレーション(up のみ)を流す。Temporal は、起動前にスキーマを作り、`temporal-namespace` の Job が、`default` の namespace を作る(Job は、完了の 10 分後に消える)。PostgreSQL と Temporal が立ち上がるまで、backend、worker、watcher は、再起動をくり返すことがある(待てば落ち着く)。
5. **確認する**: `curl https://<ホスト名>/healthz`(backend)と、`https://<ホスト名>/` の画面。最初の管理者は、`settings/secret.yaml` の名前とパスワードでログインできる(auth が、起動時に、管理者が 1 人もいなければ作る)。作ったあと、`AUTH_BOOTSTRAP_ADMIN_PASSWORD` は、Secret から消してよい。

## 運用(更新、マイグレーション、バックアップ)
更新の手順、マイグレーションのしくみと決まり、バックアップと復元、ロールバック、よくある問題は、[OPERATIONS.md](OPERATIONS.md) にまとめています。更新の前に、読んでください。

## 更新と戻し
- 更新: `kustomization.yaml` の `newTag` を、新しい `sha-…` に変えて、`kubectl apply -k .`(マイグレーションは、新しい backend と auth の Pod が起動するときに流れる)。
- 戻し: `newTag` を、前の `sha-…` に戻して、`kubectl apply -k .`。**マイグレーションは up だけ**なので、DB は戻らない(戻すときは、DB のスキーマが、古いイメージと合うかを、確かめる)。

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
- **Traefik 自体**と、プラグインの導入([docs/ingress-plugins.md](../docs/ingress-plugins.md))。
- **Temporal の認証**: Temporal は、クラスターの外に出さない前提で、認証を使わない(`TEMPORAL_ALLOW_NO_AUTH`)。Service(`temporal`)は ClusterIP のみ。

## 実行ユーザー
auth、backend、worker などは、`runAsUser: 65532`(distroless の nonroot)、frontend は、`runAsUser: 1000`(node)を、**数字で**指定している。イメージの `USER` は、名前(`nonroot`、`node`)で、kubelet は、名前の ID を確かめられず、`runAsNonRoot: true` だけでは、`container has runAsNonRoot and image has non-numeric user` で、起動できないため。

## 検証したこと
- `kubectl kustomize .` で描画でき、全てのリソースが、server-side dry-run を通る。
- PostgreSQL と Temporal は、使い捨ての namespace に、実際に立ち上げて確認した(スキーマの作成、`default` の namespace の作成、再起動して、スキーマ作成を、もう一度流しても壊れないこと)。
- **まだ**: launchs の各イメージ(backend、worker など)を使った、通しの確認(`main` への push で、イメージが GHCR に出てから)。

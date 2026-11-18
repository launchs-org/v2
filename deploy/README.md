# 本番向けのマニフェスト(kustomize)

launchs 自身を、Kubernetes に置くためのマニフェスト。この場所で `kubectl apply -k .` すると、PostgreSQL、Temporal、認証、API、ワーカー、watcher、logcollector、accessstats、フロントエンド、公開(Traefik の IngressRoute)が、namespace `launchs` に立ち上がる。

イメージは、`main` に push されるたびに、GitHub Actions(`.github/workflows/release.yml`。先に `test.yml` が通る)が、GHCR(`ghcr.io/launchs-org/v2/<名前>`)に push する。タグは `sha-<コミットの短い SHA>` と `latest`。

## 前提
- Traefik が入っている(CRD の `traefik.io` を含む。プラグインを使うなら [docs/ingress-plugins.md](../docs/ingress-plugins.md) の手順で入れる。入れていないなら、`config.yaml` の `INGRESS_STATS_PLUGIN` を空にする)。
- 既定の StorageClass があり、PVC を作れる(PostgreSQL が 20Gi を使う)。
- Traefik の前段(Cloudflare Tunnel など)で、HTTPS を終端する(このマニフェストは、Traefik の `web` の entryPoint に出す)。
- GHCR のイメージを、クラスターが取得できる(パッケージが非公開なら、`imagePullSecrets` を、各 Deployment に足す)。

## 手順
1. **秘密を作る**:
   ```sh
   cp secret.example.yaml secret.yaml
   openssl rand -hex 24                       # → POSTGRES_PASSWORD(英数字だけにする)
   openssl genpkey -algorithm ed25519         # → launchs-auth-keys の key-1.pem に貼る
   ```
   `secret.yaml` に、`POSTGRES_PASSWORD`、最初の管理者の名前とパスワード、署名鍵(必要なら Turnstile のキー)を書く。`secret.yaml` は gitignore 済み(リポジトリには置かない)。
2. **設定を直す**: `config.yaml`(ingress に払い出すホスト名のベースドメイン `INGRESS_BASE_DOMAIN` など)と、`ingressroute.yaml`(`launchs.example.com` を、自分のホスト名に。3 か所)。
3. **(任意)イメージのタグを固定する**: `kustomization.yaml` の `images` の `newTag` を、`latest` から `sha-xxxxxxx` に変える。
4. **適用する**:
   ```sh
   kubectl apply -k .
   kubectl -n launchs get pods -w
   ```
   backend と auth の initContainer が、マイグレーション(up のみ)を流す。Temporal は、起動前にスキーマを作り、`temporal-namespace` の Job が、`default` の namespace を作る(Job は、完了の 10 分後に消える)。PostgreSQL と Temporal が立ち上がるまで、backend、worker、watcher は、再起動をくり返すことがある(待てば落ち着く)。
5. **確認する**: `curl https://<ホスト名>/healthz`(backend)と、`https://<ホスト名>/` の画面。最初の管理者は、`secret.yaml` の名前とパスワードでログインできる(auth が、起動時に、管理者が 1 人もいなければ作る)。作ったあと、`AUTH_BOOTSTRAP_ADMIN_PASSWORD` は、Secret から消してよい。

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

watcher、logcollector、accessstats は、複数起動しても、advisory lock で、リーダーの 1 つだけが動く。いまは、どれも `replicas: 1`。

## 含まれないもの
- **バックアップ**: PostgreSQL のバックアップ(PVC のスナップショットや `pg_dump`)は、運用側で行う。
- **TLS と DNS**: Cloudflare などで行う(このマニフェストは、HTTP の `web` の entryPoint に出す)。
- **Traefik 自体**と、プラグインの導入([docs/ingress-plugins.md](../docs/ingress-plugins.md))。
- **Temporal の認証**: Temporal は、クラスターの外に出さない前提で、認証を使わない(`TEMPORAL_ALLOW_NO_AUTH`)。Service(`temporal`)は ClusterIP のみ。

## 検証したこと
- `kubectl kustomize .` で描画でき、全てのリソースが、server-side dry-run を通る。
- PostgreSQL と Temporal は、使い捨ての namespace に、実際に立ち上げて確認した(スキーマの作成、`default` の namespace の作成、再起動して、スキーマ作成を、もう一度流しても壊れないこと)。
- **まだ**: launchs の各イメージ(backend、worker など)を使った、通しの確認(`main` への push で、イメージが GHCR に出てから)。

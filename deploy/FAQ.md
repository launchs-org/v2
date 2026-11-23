# 導入と運用の Q&A

導入は [README.md](README.md) と [cluster/README.md](cluster/README.md)、更新・バックアップは [OPERATIONS.md](OPERATIONS.md)。ここは、**つまずいた症状から、原因と直し方を引く**ための表です(本番の Talos に入れたときの実例を含む)。

## 導入

**Q. 適用する前に、何を確かめる?**
どのクラスターに向いているかを、**必ず**確かめる。開発のクラスターと本番のクラスターで、kubeconfig が別にある。
```bash
kubectl config current-context
kubectl get nodes -o wide      # ノードの IP が、狙った環境か
```
CIDR、StorageClass、Traefik の namespace は、環境ごとに違う。調べる手順は `cluster/README.md` の「0. 調べること」。

**Q. Pod が `ImagePullBackOff`、`TLS handshake timeout` になる**
GHCR(`ghcr.io/launchs-org/v2/…`)からの取得が、遅い、または、一時的に失敗している。数分待つと、自動で再試行されて、起動することが多い(本番の導入では、全 Pod が、約 7 分で Running)。`401`、`denied` なら、パッケージが非公開。Public にするか、`imagePullSecrets` を足す。

**Q. `kubectl apply` で `would violate PodSecurity "restricted"` と出る**
警告だけで、適用は成功する(namespace の強制は `baseline`)。拒否されるのは、`hostPath` などの `baseline` に違反するときだけ。Datadog が入っているクラスターでは、admission controller が `hostPath` を注入して、拒否されることがある。その場合は、Pod に `admission.datadoghq.com/enabled: "false"` のラベルを付ける。

**Q. Postgres の Pod が `Pending`(`unbound immediate PersistentVolumeClaims`)**
PVC を作れる StorageClass が無い。`kubectl get sc` で、既定があるか確かめる。起動の直後の、数十秒の `Pending` は、PVC が束縛されるのを待っているだけ。

**Q. backend、auth が `Init:` のまま進まない**
initContainer(`migrate`)が、Postgres を待っている、または、`migrate` イメージを取得中。`kubectl -n launchs logs deploy/backend -c migrate` で見る。Postgres が Ready でなければ、先にそちら(上の Q)。

**Q. `launchs` namespace にも、Cilium の既定拒否がある**
症状は、Traefik から backend/auth/frontend に届かない(502/504)、backend が postgres/temporal に届かない。`launchs` namespace にだけ、同じ namespace の中、Traefik の namespace からの ingress(frontend 3000、backend と auth 8080)、DNS と Kubernetes API への egress を許す NetworkPolicy を足す。**クラスター全体の設定は、直さない**。本番の Talos には、この拒否は無かった。

## 公開とログイン

**Q. `https://www.launchs.org` が 302 で `…cloudflareaccess.com` に飛ぶ**
Cloudflare Zero Trust の **Access のアプリケーション**が、ホストに付いている。`www.launchs.org` と `*.launchs.org` から外す(ダッシュボードの操作)。
`curl -i https://www.launchs.org/healthz` の応答に `Www-Authenticate: Cloudflare-Access` があれば、これ。

**Q. `POST /hooks/deploy` が 403 やチャレンジの画面になる**
Cloudflare の **Bot Fight Mode** が、オン。オフにする(Free プランでは、カスタムルールでスキップできない)。トークン無しで **401** なら、backend まで届いている(正常)。

**Q. 公開は 404(`404 page not found`)**
Traefik まで届いている。次を確かめる: Tunnel の公開ホスト名が、Traefik の HTTP を向いているか、IngressRoute の `Host(...)` が、公開のホスト名と同じか(`ingressroute.yaml`)、`TRAEFIK_ENTRY_POINTS` が、Traefik の entryPoint 名(`web`)と同じか。クラスターの中からの確認:
```bash
kubectl -n launchs run curltest --image=curlimages/curl --restart=Never --rm -i --command -- \
  curl -si http://traefik.traefik.svc/healthz -H "Host: www.launchs.org"
```
これが 200 なら、クラスターの中は正しく、原因は Cloudflare の側。

**Q. ログインできない**
- 最初の管理者は、`launchs-secret` の `AUTH_BOOTSTRAP_ADMIN_NAME` と `AUTH_BOOTSTRAP_ADMIN_PASSWORD`。**管理者が 1 人もいないときだけ**、auth が起動時に作る(後でパスワードを変えても、作り直されない)。
- Turnstile を有効にした(`AUTH_TURNSTILE_SITE_KEY` と `AUTH_TURNSTILE_SECRET_KEY`)なら、Cloudflare の Turnstile のウィジェットの**許可ホスト名**に、`www.launchs.org` が必要。無ければ、確認で弾かれる。
- サインアップは、`AUTH_SIGNUP_MODE=invite`(招待制)が既定。
- 管理者の作成や昇格は、`OPERATIONS.md` §7。

**Q. code-server などの WebSocket が `1006` で切れる**
Cloudflare 経由で、元のスキーム(https)とクライアント IP を Traefik が受け取れていない。Traefik に `launchs-edge` を入れる(`cluster/README.md` §3)。

## 運用

**Q. 画面のメトリクス(CPU、メモリ)が `null`**
metrics-server が無い(`cluster/README.md` §2)。入れた直後は、一部のノードが `kubectl top nodes` で `<unknown>` になることがある。少し待つ。

**Q. アクセスログと集計が出ない**
次を、順に確かめる。
1. `kubectl -n traefik logs deploy/traefik | grep -i plugins` に、`launchs-edge` と `launchs-stats` の読み込みが出ているか。
2. `INGRESS_STATS_PLUGIN` が `launchs-stats` で、worker を再起動したか。
3. **既存の ingress には、Middleware が自動で付かない**。ingress を、もう一度反映する(または、`accessStats` を切り替える)。反映の前のアクセスは、記録されない。
4. accessstats のログ(`kubectl -n launchs logs deploy/accessstats`)に、Traefik の Pod を追い始めた、とあるか。

**Q. 設定(ConfigMap、Secret)を変えたのに、反映されない**
環境変数は、Pod の起動時に読まれる。`kubectl apply -k settings/` のあと、該当の Deployment を再起動する。
```bash
kubectl -n launchs rollout restart deploy/auth      # 例
```
`API_TOKEN_SECRET` を変えると、**発行済みの API トークンが、全て使えなくなる**。

**Q. 利用者が、volume(RWX)を使うプロジェクトを作ると、起動が遅い**
Longhorn の RWX は、NFS の共有(share-manager)を起動するので、初回は、3 分前後かかることがある。

**Q. メモリやノードに余裕が無い**
`kubectl -n launchs scale deploy/<名前> --replicas=<数>` で、一時的に下げる(恒久的には、`kustomization.yaml` の `replicas`)。watcher、logcollector、accessstats は、リーダー選出なので、複数でよい。

**Q. 更新するときの順序は?**
`OPERATIONS.md` §3(バックアップ → 差分 → 適用 → 確認)。**列を足すマイグレーションの後は、watcher と worker を再起動する**(`cached plan must not change result type` を避ける)。

**Q. 更新を戻したい**
launchs 本体は `OPERATIONS.md` §6。Traefik は `helm rollback traefik <前の revision> -n traefik`(`helm history traefik -n traefik`)。

**Q. Traefik を更新(再起動)すると、どうなる?**
1 レプリカなら、数秒、**このクラスターの公開が、全部**止まる。`--atomic` を付けて、失敗したら、自動で戻す。時間の少ない時間帯に行う。

**Q. 旧 launchs(namespace `launchs-org`)を消してよい?**
先にデータを確認する。DB の PVC が `local-path`(reclaim が Delete)なので、namespace や PVC を消すと、**データが戻らず消える**。移すか、不要と確かめてから消す。旧プロジェクトの IngressRoute(`*.launchs.org`)も、残っていれば、同じ。

**Q. `local-path` の PVC は、危ない?**
ノードのローカルディスクに載る。そのノードが壊れると、失う。重要なデータは、Longhorn(複製あり)に載せる。launchs 本体の Postgres と、利用者の volume は、Longhorn。

**Q. バックアップは?**
`OPERATIONS.md` §5(`pg_dump` と、Longhorn のスナップショット)。更新の前に、必ず取る。復元の手順も、同じ場所。

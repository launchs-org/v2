# クラスター側の前提(launchs の外)

`deploy/`(本体と settings)だけでは足りない、**クラスター全体に 1 回だけ入れるもの**。launchs の namespace には、含めない。
本番の Talos(`launchs.org`)に入れた手順の記録(2026-11-22)。次の順に行う。

## 0. 調べること(推測しない)
```bash
kubectl get nodes -o wide
kubectl cluster-info dump | grep -m3 -E "cluster-cidr|service-cluster-ip-range"
kubectl get sc
kubectl get pods -A | grep -iE "traefik|cloudflared|metrics-server|datadog"
helm list -n traefik
```
本番の Talos の値: Pod `10.244.0.0/16`、Service `10.96.0.0/12`、ノード `192.168.10.161-165`、StorageClass `longhorn`(既定)と `local-path`、Traefik は chart 41.6.1(namespace `traefik`、entryPoint `web`=8000)、入口は Cloudflare Tunnel(cloudflared)。Datadog は無い。

## 1. 環境ごとの設定(`deploy/settings/`)
`settings/config.launchs.org.example.yaml` が、本番の値の例(適用の対象ではない)。`config.yaml` に写して使う。

| キー | 本番の値 | 理由 |
|---|---|---|
| `INGRESS_BASE_DOMAIN` | `launchs.org` | 公開ドメイン。IngressRoute の Host(`ingressroute.yaml`)も `www.launchs.org` にする |
| `AUTH_TRUSTED_PROXY_CIDRS` | `10.244.0.0/16` | Traefik の Pod の範囲(広すぎると、クライアント IP を偽装される) |
| `VOLUME_STORAGE_CLASS` | `longhorn` | 利用者の volume(RWX)。Postgres の PVC も、既定の `longhorn` |
| `INGRESS_STATS_PLUGIN` | `launchs-stats` | §3 のプラグインを入れたあとだけ。**入れていない Traefik では、空にする** |

secret は `settings/secret.example.yaml` から作る(コミットしない)。`launchs-secret` の `POSTGRES_PASSWORD`(`openssl rand -hex 24`)、`API_TOKEN_SECRET`(`openssl rand -hex 64`)、最初の管理者、Turnstile のキー。`launchs-auth-keys` は `openssl genpkey -algorithm ed25519`。

## 2. metrics-server(`metrics-server.yaml`)
無いと、画面のメトリクスの CPU とメモリが null になる。Talos の kubelet の証明書は自己署名なので、`--kubelet-insecure-tls` が要る(同梱のマニフェストに入っている)。
```bash
kubectl apply -f cluster/metrics-server.yaml
kubectl -n kube-system rollout status deploy/metrics-server
kubectl top nodes
```
適用の直後は、一部のノードが `<unknown>` になることがある(少し待つ)。

## 3. Traefik のプラグイン(launchs-edge と launchs-stats)
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

## 4. Cloudflare(ダッシュボード。手作業)
- Tunnel の公開ホスト名に、`www.launchs.org` と `*.launchs.org` を、Traefik の HTTP(`http://traefik.traefik:80`)で向ける。
- **Zero Trust の Access のアプリケーションは、`www.launchs.org` と `*.launchs.org` に付けない**(付けると、ログイン画面に飛ばされ、利用者もアプリも開けない)。
- **Bot Fight Mode は、オフ**(オンだと、GitHub Actions の `POST /hooks/deploy` が Managed Challenge で止まる。Free プランでは、スキップできない)。
- Turnstile を使うなら、ウィジェットの許可ホスト名に `www.launchs.org` を入れる。

## 5. 適用と確認
`deploy/README.md` の順序(namespace → settings → 本体)で適用する。
```bash
kubectl -n launchs get pods                       # 全て Running(GHCR からの取得に数分かかることがある)
curl -i https://www.launchs.org/healthz           # 200
curl -i -X POST https://www.launchs.org/hooks/deploy -d "{}"   # 401(backend まで届く)
```

## 6. 既存の古いものの扱い
旧 launchs(namespace `launchs-org`)は、DB の PVC が `local-path`(削除するとデータが消える)なので、**IngressRoute だけ**を外し、あとは残した。残したものを消すときは、先にデータを移す。

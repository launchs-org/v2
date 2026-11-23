# 最初のデプロイと、更新

## 最初のデプロイ

### 前提
- Kubernetes(3 ノード)と、**Argo CD**(namespace `argocd`)。
- 既定の StorageClass(PVC を作れる。PostgreSQL が 20Gi を使う)。
- Cloudflare Tunnel のトークン(入口。HTTPS は、Cloudflare が終端する)。
- `kubectl`(接続先が、入れたいクラスター)と `openssl`。

### 1. 設定を直す(環境ごとの値)
| ファイル | 直すところ |
|---|---|
| `settings/config.yaml` | `INGRESS_BASE_DOMAIN`(払い出すドメイン)、`AUTH_TRUSTED_PROXY_CIDRS`(Traefik の Pod の範囲)、`VOLUME_STORAGE_CLASS` |
| `ingressroute.yaml` | `Host`(launchs 本体のホスト名。例: `www.launchs.org`) |
| `argocd/applications.yaml` | Pod の範囲が `10.244.0.0/16` でないクラスターは、`launchs-cluster` の `kustomize.patches` を有効にする(`launchs-edge` の `trustedCIDRs`) |

本番(launchs.org)の値の例は `settings/config.launchs.org.example.yaml`。調べ方は [description.md](description.md)。

### 2. 実行する
```bash
cd deploy
TUNNEL_TOKEN='<トンネルのトークン>' ./setup.sh
```
`setup.sh` が、次を行う(何度実行しても、同じ結果になる)。
1. namespace、秘密(`settings/secret.yaml`。無ければ、乱数で作る)、設定を適用する
2. cloudflared のトークンの Secret を作る
3. Argo CD の Application を作り、**traefik → launchs-cluster → launchs-prod** の順に同期する(Traefik とプラグイン、metrics-server、cloudflared、launchs 本体が入る)

`./setup.sh --secrets-only`(秘密を作るだけ)と `--no-sync`(同期は、Argo CD の画面で行う)もある。`-h` で使い方。

### 3. Cloudflare の手作業
- Tunnel の公開ホスト名に、`www.launchs.org` と `*.launchs.org` を、`http://traefik.traefik:80` で向ける。
- Zero Trust の Access は、この 2 つに付けない。
- Bot Fight Mode は、オフ(オンだと、GitHub Actions の webhook が止まる)。

### 4. 確認する
```bash
kubectl -n launchs get pods                                      # 全て Running
curl -i https://www.launchs.org/healthz                          # 200
curl -i -X POST https://www.launchs.org/hooks/deploy -d "{}"     # 401(backend まで届く)
```
最初の管理者の名前とパスワードは、`settings/secret.yaml` の `AUTH_BOOTSTRAP_ADMIN_NAME` / `AUTH_BOOTSTRAP_ADMIN_PASSWORD`。

## 更新

### launchs 本体
1. 変更を `main` に入れる。CI が、イメージを GHCR に出し、`kustomization.yaml` の `newTag`(`sha-…`)を、`main` に自動でコミットする。
2. 同期する(Argo CD の画面、または次)。マイグレーションは、新しい backend と auth の Pod が、起動時に流す。
   ```bash
   kubectl -n argocd annotate application launchs-prod argocd.argoproj.io/refresh=hard --overwrite
   kubectl -n argocd patch application launchs-prod --type merge -p '{"operation":{"sync":{}}}'
   kubectl -n launchs rollout status deploy/backend
   ```
   Argo CD を使わないときは、`git pull` して、`kubectl apply -k .`(この `deploy/` で)。

### 設定(`settings/`)を変えたとき
```bash
kubectl apply -k settings/
kubectl -n launchs rollout restart deploy      # 該当の Deployment だけでもよい
```

### Traefik とプラグイン
`traefik-plugins/` を変えたら、values を作り直して、一緒にコミットする(CI が、ずれを検査する)。そのあと `traefik` の Application を同期する。
```bash
python3 traefik-plugins/render_helm_values.py --full > deploy/cluster/traefik-values.yaml
```

### 戻す
`newTag` を前の `sha-…` に戻して、同期する。**マイグレーションは up だけ**なので、DB は戻らない。更新の前の確認、バックアップ、戻し方の詳細は [operation.md](operation.md)。

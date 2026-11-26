# 最初のデプロイと、更新

## 最初のデプロイ

### 前提
- Kubernetes(3 ノード)と、**Argo CD**(入れ方は [argocd.md](argocd.md))。
- 既定の StorageClass(PVC を作れる。PostgreSQL が 20Gi を使う)。
- Cloudflare Tunnel のトークン(入口。HTTPS は、Cloudflare が終端する)。
- `kubectl`(接続先が、入れたいクラスター)と `openssl`。

### 1. 環境ごとの値を決める
Git の値は既定。環境ごとに違う値は、**Argo CD の Application で上書きする**([argocd.md](argocd.md) の「環境ごとの値を、Argo CD で上書きする」)。

| 値 | 内容 |
|---|---|
| `LAUNCHS_HOST` | launchs 本体のホスト名(例: `www.launchs.org`)。IngressRoute の Host も、これで変わる |
| `INGRESS_BASE_DOMAIN` | ingress に払い出すドメイン |
| `VOLUME_STORAGE_CLASS`、`AUTH_TRUSTED_PROXY_CIDRS` | StorageClass と、Traefik の Pod の範囲 |
| `launchs-edge` の `trustedCIDRs` | cloudflared の Pod の範囲(Git の値は `10.244.0.0/16`) |

本番(launchs.org)の値の例は `settings/config.launchs.org.example.yaml`。調べ方は [description.md](description.md)。

### 2. Argo CD で入れる
手順は [argocd.md](argocd.md)。流れ:
1. Argo CD を入れる(入っていなければ)
2. 秘密と設定を作る(`settings/secret.yaml`、cloudflared のトークン)
3. `kubectl apply -f argocd/applications.yaml`(Application を作る)
4. **traefik → launchs-cluster → launchs-prod** の順に同期する(Traefik とプラグイン、metrics-server、cloudflared、launchs 本体が入る)

### 2.5 アクセスログの転送を使うとき(任意)
webhook と Google Drive への書き出しは、Secret に鍵を足すまで、**無効のまま**です。使うときは、[operation.md](operation.md) の「アクセスログの転送(exporter)を有効にする」(Secret の 3 つのキーと、Google Cloud 側の準備。スコープは `drive.file` と `openid email` だけ)に従ってください。

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
2. `launchs-prod` を同期する(Argo CD の画面、または次)。マイグレーションは、新しい backend と auth の Pod が、起動時に流す。
   ```bash
   kubectl -n argocd annotate application launchs-prod argocd.argoproj.io/refresh=hard --overwrite
   kubectl -n argocd patch application launchs-prod --type merge -p '{"operation":{"sync":{}}}'
   kubectl -n launchs rollout status deploy/backend
   ```
   Argo CD を使わないときは、`git pull` して、`kubectl apply -k .`(この `deploy/` で)。

### 設定を変えたとき
- ConfigMap(`settings/config.yaml`、または Argo CD の上書き)を変えて、`launchs-prod` を同期する。**Pod は再起動しない**ので、`kubectl -n launchs rollout restart deploy`(該当の Deployment だけでもよい)。
- 秘密(`settings/secret.yaml`)を変えたときは、`kubectl apply -k settings/` のあと、同じく再起動する。

### Traefik とプラグイン
`traefik-plugins/` を変えたら、values を作り直して、一緒にコミットする(CI が、ずれを検査する)。そのあと `traefik` の Application を同期する。
```bash
python3 traefik-plugins/render_helm_values.py --full > deploy/cluster/traefik-values.yaml
```

### 戻す
`newTag` を前の `sha-…` に戻して、同期する。**マイグレーションは up だけ**なので、DB は戻らない。更新の前の確認、バックアップ、戻し方の詳細は [operation.md](operation.md)。

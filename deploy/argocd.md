# Argo CD の設定

launchs と、その土台(Traefik とプラグイン、metrics-server、cloudflared)は、Argo CD の Application で入る(`argocd/applications.yaml`)。このファイルは、Argo CD を入れるところから、全部が動くところまで。

| Application | 内容 | 場所 |
|---|---|---|
| `traefik` | Traefik(Helm のチャート 42.3.2)と、プラグイン、基本の設定 | チャート + `cluster/traefik-values.yaml` |
| `launchs-cluster` | metrics-server、launchs-edge の Middleware、cloudflared、`traefik` の namespace | `cluster/`(kustomize) |
| `launchs-prod` | launchs 本体 | `deploy/`(kustomize) |

全て `main` ブランチを見る。**同期は手動**(自動にはしていない)。

## 1. Argo CD を入れる(入っていないとき)
```bash
kubectl create namespace argocd
# 版は固定する(動作を確認した版: v3.6.2)。CRD が大きいので、server-side apply にする
kubectl apply -n argocd --server-side -f https://raw.githubusercontent.com/argoproj/argo-cd/v3.6.2/manifests/install.yaml
kubectl -n argocd rollout status deploy/argocd-server
```
画面を開く(外には出さない前提。手元から):
```bash
kubectl -n argocd port-forward svc/argocd-server 8080:443      # https://localhost:8080
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d   # admin の初期パスワード
```
初期パスワードは、変えたあと、Secret `argocd-initial-admin-secret` を消す。

## 2. リポジトリ
`https://github.com/launchs-org/v2` が**公開**なら、設定は要らない。非公開にしたときは、読み取りの認証情報を、Argo CD に登録する。
```bash
kubectl -n argocd create secret generic launchs-repo \
  --from-literal=type=git --from-literal=url=https://github.com/launchs-org/v2 \
  --from-literal=username=<ユーザー名> --from-literal=password='<読み取りの token>'
kubectl -n argocd label secret launchs-repo argocd.argoproj.io/secret-type=repository
```

## 3. 秘密と設定を作る(Git に入れない。Application の前に)
```bash
cd deploy
kubectl apply -f namespace.yaml                                   # namespace launchs
cp settings/secret.example.yaml settings/secret.yaml              # 値を入れる(下)
kubectl apply -k settings/                                        # 設定(ConfigMap)と秘密
kubectl create namespace cloudflared
kubectl -n cloudflared create secret generic tunnel-token --from-literal=token='<トンネルのトークン>'
```
`settings/secret.yaml` に入れる値(`openssl` で作る):

| キー | 作り方 |
|---|---|
| `POSTGRES_PASSWORD` | `openssl rand -hex 24`(英数字だけ) |
| `API_TOKEN_SECRET` | `openssl rand -hex 64` |
| `AUTH_BOOTSTRAP_ADMIN_NAME` / `_PASSWORD` | 最初の管理者(名前と、長い乱数のパスワード) |
| `launchs-auth-keys` の `key-1.pem` | `openssl genpkey -algorithm ed25519` の出力(PEM の 3 行) |

環境ごとの設定の値(ドメイン、Pod の範囲、StorageClass)は、[deploy.md](deploy.md) の「設定を直す」。

## 4. Application を作る(1 回だけ)
```bash
kubectl apply -f deploy/argocd/applications.yaml
kubectl -n argocd get applications          # traefik、launchs-cluster、launchs-prod(OutOfSync で正しい)
```

## 5. 同期する(順序: traefik → launchs-cluster → launchs-prod)
Argo CD の画面で、各 Application の「SYNC」を押す。または:
```bash
for app in traefik launchs-cluster launchs-prod; do
  kubectl -n argocd annotate application $app argocd.argoproj.io/refresh=hard --overwrite   # Git の最新を取り込む
  kubectl -n argocd patch application $app --type merge -p '{"operation":{"sync":{}}}'
  kubectl -n argocd get application $app -w          # SYNC STATUS: Synced、HEALTH STATUS: Healthy になったら、Ctrl-C で次へ
done
```
- **順序が大事**: `launchs-cluster` の Middleware と、`launchs-prod` の IngressRoute は、Traefik の CRD が先に要る(`SkipDryRunOnMissingResource` で、検査では止まらないが、先に `traefik` を同期する)。
- 全部が Synced / Healthy になったら、[deploy.md](deploy.md) の「確認する」。

## 6. 日々の使い方
- **更新**: `main` に入れる → CI が `newTag` をコミットする → `launchs-prod` を同期する([deploy.md](deploy.md) の「更新」)。
- **差分を見る**: 画面の「APP DIFF」、または `kubectl -n argocd get application <名前>`(OutOfSync なら、Git と違う)。
- **戻す**: Git で `newTag` を戻して、同期する(画面の「History and Rollback」は、Git と食い違うので、使わない)。
- **自動で同期したいとき**: `applications.yaml` の `syncPolicy` に `automated: {selfHeal: true}` を足す。**`prune` は付けない**(Git から消したものを、クラスターからも消してしまう。PostgreSQL の PVC などが対象になる)。

## 7. 注意と、つまずき
- **既に Helm で入れた Traefik がある**: 同じ版・同じ values なら、描画は同じ。同期しても、中身は変わらず、Argo CD が管理を引き継ぐ(Helm の release の記録は残る)。
- **`trustedCIDRs`**: `launchs-edge` が信じる接続元(cloudflared の Pod の範囲)。Git の値は `10.244.0.0/16`。違う範囲のクラスターは、`applications.yaml` の `launchs-cluster` の `kustomize.patches`(コメントの例)で上書きする。合っていないと、クライアント IP と https が、アプリに届かない。
- **プラグインのソースを変えたとき**: `python3 traefik-plugins/render_helm_values.py --full > deploy/cluster/traefik-values.yaml` で作り直して、コミットする(CI が、ずれを検査する)。
- **秘密は、Git に入れない**: `settings/secret.yaml` と `tunnel-token` は、Argo CD の管理の外(手で作る)。`launchs-prod` の Application は、`settings/` を含めない。

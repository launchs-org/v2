# deploy/ — 本番向けのマニフェスト(kustomize + Argo CD)

launchs 本体と、その土台(Traefik とプラグイン、metrics-server、cloudflared)を、クラスターに入れる。

入れ方は、Argo CD の Application(`argocd/applications.yaml`)で、[argocd.md](argocd.md) → [deploy.md](deploy.md) の順に読む。

| 文書 | 内容 |
|---|---|
| [argocd.md](argocd.md) | **Argo CD の設定**(導入、秘密、Application、同期) |
| [deploy.md](deploy.md) | **最初のデプロイと、更新の方法** |
| [operation.md](operation.md) | 運用(マイグレーション、バックアップ、戻し方) |
| [questions.md](questions.md) | つまずいたときの Q&A(症状から引く) |
| [description.md](description.md) | 構成、権限、高可用性、クラスター側の前提 |

| 場所 | 内容 |
|---|---|
| `*.yaml`、`kustomization.yaml` | 本体(PostgreSQL、Temporal、auth、backend、worker、watcher、logcollector、accessstats、frontend、IngressRoute) |
| `settings/` | 環境ごとの設定(ConfigMap と、Secret。`secret.yaml` は Git に入れない) |
| `cluster/` | クラスター全体に 1 回のもの(metrics-server、launchs-edge の Middleware、cloudflared、Traefik の values) |
| `argocd/` | Argo CD の Application(traefik、launchs-cluster、launchs-prod) |

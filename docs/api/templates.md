# テンプレート

よく使う構成(deploy、volume、ConfigMap、service、ingress)を、変数を埋めるだけで、まとめて作る。仕様は [templates.md](../endpoints/templates.md)、設計は [docs/templates.md](../templates.md)。

| メソッド | パス | 説明 |
|---|---|---|
| `GET` | `/templates` | 一覧(名前、説明、分類、既定の版) |
| `GET` | `/templates/:templateId?version=` | 詳細(変数、版) |
| `POST` | `/projects/:projectId/templates/:templateId/apply` | 作る(`version` を選べる) |
| `GET` | `/projects/:projectId/template-applies/:applyId` | 適用の進み具合 |

## 流れ

1. `GET /templates/:templateId` で、変数(名前、型、必須、既定値)を知る。
2. `POST …/apply` で、変数と版を送る。**変数の検査とクォータの確認**をして、計画を保存し、Workflow が、順に作る。
3. `GET …/template-applies/:applyId` で、進み具合を見る。

## 落とし穴

- 途中で失敗したら、そこで**止まる**。**作れたものは残る**(巻き戻さない)。失敗の理由は、タスクに出る。
- 作ったものには、`template:<id>` と `apply:<識別>` のタグが付く。**このタグで、まとめて削除できる**([tags-metrics.md](tags-metrics.md))。ただし、ボリュームは、個別に消す。
- クォータを超える適用は、**始める前に**、409 で断られる。

# プロジェクト

資源(deploy、ボリューム、Ingress など)は、全て、プロジェクトに属する。プロジェクトは、k8s の namespace に対応し、ネットワークが隔離される。仕様は [projects.md](../endpoints/projects.md)。

| メソッド | パス | 説明 |
|---|---|---|
| `POST` | `/projects` | 作成 `{"name": "my-app"}` → `{"projectId"}`。名前は英数字とハイフン、1〜64 文字。同じユーザーで重複は 409 |
| `GET` | `/projects` | 一覧(ページ送り) |
| `GET` | `/projects/:projectId` | 取得 |
| `PATCH` | `/projects/:projectId` | 名前の変更 |
| `DELETE` | `/projects/:projectId` | 削除(中のものも全て。非同期) |
| `GET` | `/projects/:projectId/events` | イベント(k8s の出来事。起動しない原因の手がかり) |
| `GET` | `/projects/:projectId/tasks` | タスク(Workflow の履歴。失敗の理由が分かる)。`/tasks/:taskId` で詳細 |
| `GET` | `/tasks` | 全プロジェクトのタスク |
| `GET` | `/quota` | 自分の上限と、使用量 |

## 注意

- 状態が `active` でないプロジェクト(削除中など)には、資源を作れない(409)。
- **上限(クォータ)**: deploy の数、スペックごとの数、ボリュームの大きさなど。超えると 409。作る前に `GET /quota` で見られる。[quota.md](../endpoints/quota.md)
- 失敗したとき、まず `GET /projects/:projectId/tasks` の理由を見る。

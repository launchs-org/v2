# プロジェクトの Event(namespace で起きた k8s の Event)

プロジェクトの namespace で起きたこと(イメージを取れない、スケジュールできない、OOM で落ちた、など)を、画面から見られるようにする。k8s の Event は、約 1 時間で消えるので、DB に溜める。

## 仕組み

1. **watcher が保存する**(書き手は watcher だけ。[watcher.md](watcher.md) の「プロジェクトの Event」)。`project-` + UUID の namespace の `Event` を観測し、`project_events` に upsert する。
2. **backend は、DB を読むだけ**(k8s に問い合わせない)。API は、[endpoints/projects.md](endpoints/projects.md) の「イベント一覧」を参照。

## テーブル `project_events`(`migrations/app/000013_create_project_events.up.sql`)

| 列 | 中身 |
|---|---|
| `id` | 通し番号(同じ時刻の並びと、カーソルに使う) |
| `project_id` | namespace(プロジェクトの ID)。プロジェクトの行への外部キーは、持たない(削除のあとも、残して、保存期間で掃除するため) |
| `event_uid` | k8s の Event の UID。**一意**。同じ Event は、回数を増やして更新される |
| `type` | `Normal` か `Warning` |
| `reason`、`message` | 理由(`Pulling`、`BackOff`、`FailedScheduling` など)と本文 |
| `object_kind`、`object_name` | 対象(Pod、Deployment、ReplicaSet、PersistentVolumeClaim など) |
| `deploy_id` | 対象が deploy に属するときの deploy の ID。属さなければ空 |
| `count` | 同じ Event が、何回起きたか |
| `first_seen_at`、`last_seen_at` | 最初と最後に起きた時刻 |

## 保存期間

- **7 日**(ログと同じ)。最後に起きた時刻が、これより古いものを削除する。
- **1 プロジェクトにつき 5000 件**まで。超えたら、古い(最後に起きた時刻の古い)ものから削除する。
- watcher のリーダーが、1 時間ごとに掃除する。
- k8s から Event が消えても、DB には残る。deploy を消しても、残る。

## 注意

- k8s の Event は、**発行元によって時刻の項目が違う**(`firstTimestamp` / `lastTimestamp` / `eventTime`)。順に探し、全て無ければ、作成時刻を使う。
- **Event は、完全な記録ではない**。k8s が、同じ内容を、まとめたり、間引いたりする。watcher が止まっていた間に、k8s からも消えた Event は、拾えない。

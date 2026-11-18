[← エンドポイント一覧](README.md)

## タスク関連 (/projects/:projectId/tasks)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている。設計は [docs/tasks.md](../tasks.md)。

### タスク一覧
- Get: /projects/:projectId/tasks
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query(全て任意)
```json
{"limit": 100, "cursor": "<前回の nextCursor>", "status": "failed", "kind": "deploy_create"}
```
- Response (200)
```json
{
  "tasks": [
    {
      "taskId": "task-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "kind": "deploy_create",
      "resourceId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "failed",
      "error": "イメージを取得できません: ghcr.io/example/app:missing (not found)",
      "startedAt": "2026-11-18T10:00:00Z",
      "finishedAt": "2026-11-18T10:05:00Z",
      "durationMs": 300000,
      "workflowId": "create-deploy-…"
    }
  ],
  "nextCursor": null,
  "counts": {"running": 1, "failedRecent": 2}
}
```
- Note: Workflow(裏で動く処理)の履歴。**トップのタスクだけ**を、新しい順に返す(テンプレートの適用が呼ぶ deploy の作成などの、子のタスクは、詳細で返す)
- Note: `status` は `running`、`succeeded`、`failed`、`canceled`、`lost`。`lost` は、実行中のまま 2 時間を過ぎて、終わりの記録が付いていないもの(絞り込みの `running` には含まない)。それ以外の `status` は 400
- Note: `kind` は、`deploy_create`、`deploy_delete`、`deploy_config`、`deploy_files`、`deploy_mounts`、`deploy_configmap_mounts`、`deploy_scale`、`deploy_crash_stop`、`service_create`、`service_delete`、`service_ports`、`volume_resize`、`volume_delete`、`ingress_apply`、`ingress_reapply`、`ingress_delete`、`template_apply`、`project_delete`。それ以外は 400
- Note: `limit` は 1〜100(既定 20)。範囲外の `limit` と不正な `cursor` は 400。`nextCursor` は、最後のページなら null
- Note: `error` は、失敗の理由(失敗したときだけ。秘密を含みうる部分は伏せてあり、500 文字まで)。`durationMs` は、実行中なら、いままでの時間。`workflowId` は、運用で Temporal の画面を探すときに使う
- Note: `counts` は、プロジェクトの、実行中のタスクの数と、直近 24 時間に失敗したタスクの数(絞り込みに関係なく、トップのタスクを数える)
- Note: 保存は、30 日と、プロジェクトごとに 1000 件の、どちらか早い方まで
- Note: 他のユーザーのプロジェクトは 404

### タスク取得
- Get: /projects/:projectId/tasks/:taskId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200): 一覧の 1 件と同じ形に、`children`(子の Workflow のタスク。開始の昇順。無ければ空の配列)を足したもの
- Note: 他のユーザーのプロジェクト、存在しないタスクは 404

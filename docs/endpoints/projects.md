[← エンドポイント一覧](README.md)

## プロジェクト関連 (/projects)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/projects` の形)。
### プロジェクト作成
- Post: /projects
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "my-app"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
```
- Note: build/deploy は必ずいずれかの projectId に属する。以後の /build /deploy は projectId を伴って呼び出す
- Note: `name` は英数字とハイフンのみ、1〜64文字(最小長は仮)。違反は 400。同じユーザーの中で重複した場合は 409(仮)

### プロジェクト一覧
- Get: /projects
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query(どちらも任意)
```json
{"limit": 20, "cursor": "<前回の nextCursor>"}
```
- Response (200)
```json
{"projects": [{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app", "status": "active"}], "nextCursor": null}
```
- Note: `status` はプロジェクトの状態。`"active"` は通常の状態、`"deleting"` は削除を受け付けて実体を削除している最中の状態、`"delete_failed"` は削除がリトライの期限までに終わらず止まっている状態(もう一度 DELETE を呼ぶとやり直せる)
- Note: 作成日時の降順で返す。`limit` は1〜100(デフォルト20)。範囲外の `limit` と不正な `cursor` は 400(仮)。`cursor` はクライアントが解釈しない文字列で、`nextCursor` は最後のページなら null、続きがあれば文字列

### プロジェクト取得
- Get: /projects/:projectId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app", "status": "active"}
```
- Note: `status` はプロジェクトの状態。`"active"` は通常の状態、`"deleting"` は削除を受け付けて実体を削除している最中の状態、`"delete_failed"` は削除がリトライの期限までに終わらず止まっている状態(もう一度 DELETE を呼ぶとやり直せる)

### プロジェクト build 履歴一覧
- Get: /projects/:projectId/builds
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query(どちらも任意)
```json
{"limit": 20, "cursor": "<前回の nextCursor>"}
```
- Response (200)
```json
{
  "builds": [
    {
      "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "success",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T09:00:00+09:00"
    }
  ],
  "nextCursor": null
}
```
- Note: 作成日時の降順で返す。`limit` は1〜100(デフォルト20)。範囲外の `limit` と不正な `cursor` は 400(仮)。`cursor` はクライアントが解釈しない文字列で、`nextCursor` は最後のページなら null、続きがあれば文字列

### プロジェクト deploy 履歴一覧
- Get: /projects/:projectId/deploys
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query(どちらも任意)
```json
{"limit": 20, "cursor": "<前回の nextCursor>"}
```
- Response (200)
```json
{
  "deploys": [
    {
      "deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "active",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T09:00:00+09:00"
    }
  ],
  "nextCursor": null
}
```
- Note: 作成日時の降順で返す。`limit` は1〜100(デフォルト20)。範囲外の `limit` と不正な `cursor` は 400(仮)。`cursor` はクライアントが解釈しない文字列で、`nextCursor` は最後のページなら null、続きがあれば文字列
- Note: deploy は削除が完了するとレコードごと消えるため、削除済みの deploy は含まれない。削除中(status が "deleting")のものは含まれる(仮)

### プロジェクト更新
- Patch: /projects/:projectId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "my-app-renamed"}
```
- Response (200)
```json
{}
```
- Note: `name` の制約と重複の扱いは [プロジェクト作成](#プロジェクト作成) と同じ。プロジェクトの status が "deleting" または "delete_failed" の間は 409

### プロジェクト削除
- Delete: /projects/:projectId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: プロジェクト削除時、配下の build 履歴・イメージ・deploy(稼働中のものを含む)・ingress・volume(データごと)は全て連鎖して削除される。k8s の namespace(プロジェクトごとに1つ、名前は projectId)ごと削除するので、PVC のデータも消える
- Note: 呼び出すと即座に応答し、status は直ちに "deleting" になる。実体の削除は Workflow で非同期に行い、完了するとレコード自体が削除される(以後の GET は 404 を返す)
- Note: deleting の間は、変更系の操作と再度の削除は 409 を返す。GET は status が "deleting" の状態を返す
- Note: 削除の Workflow は、k8s などの一時的な障害ならリトライして完了させる。リトライの期限(15分、仮)までに終わらなければ、status を "delete_failed" にして止まる。このとき、実体は一部だけ削除されている可能性がある。"delete_failed" のプロジェクトにもう一度 DELETE を呼ぶと、受け付けられて status が "deleting" に戻り、削除をやり直す(冪等なので、途中まで消えていても問題ない)。"delete_failed" の間も、変更系の操作は 409

[← プロジェクト関連](README.md)

# プロジェクト削除

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Delete /projects/:projectId`

## 説明

プロジェクトを削除するエンドポイント。配下の build 履歴・イメージ・deploy(稼働中のものを含む)・ingress・volume(データごと)は全て連鎖して削除される。削除は非同期で行われ、受け付けた直後は `status` が `"deleting"` になる。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 自分が所有する `projectId` を指定して削除を受け付けると 200 が返り、[プロジェクト取得](003-get-projects-projectid.md) の `status` が即座に `"deleting"` になること
- 削除が完了したら、[プロジェクト取得](003-get-projects-projectid.md) が 404 になること
- 削除完了後、配下の build 履歴・イメージ・稼働中だった deploy(およびその service/volumeマウント)・ingress・volume(データごと)が全て連鎖削除されていること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `deleting` の間の変更系操作(プロジェクト更新など)は 409 が返ること
- `deleting` の間の再度の DELETE は 409 が返ること
- 削除がリトライの期限までに終わらなかった場合、status が `"delete_failed"` になり、GET でそれが見えること
- `"delete_failed"` の間の変更系操作(プロジェクト更新など)は 409 が返ること
- `"delete_failed"` のプロジェクトに DELETE を呼ぶと 200 が返り、status が `"deleting"` に戻って、削除がやり直されること(やり直しの開始に失敗したときは、`"delete_failed"` のまま 500)
- `deleting` のプロジェクトの配下に build・deploy・ingress・volume を作ろうとした場合 409 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 稼働中の deploy が多数ある場合でも、DELETE は即座に 200 を返し、実体の削除は非同期に完了すること
- 配下の volume は、マウントの有無にかかわらず、PVC のデータごと削除されること(k8s の namespace ごと削除する)

## 仕様

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
- Note: 削除がリトライの期限(15分、仮)までに終わらなければ、status は "delete_failed" になって止まる。この状態からもう一度 DELETE を呼ぶと、受け付けられて削除をやり直す。"delete_failed" の間も、変更系の操作は 409

## 参照

- [dev-docs/endpoints/projects.md](../../endpoints/projects.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

[← エンドポイント一覧](README.md)

## ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
### ボリューム作成
- Post: /projects/:projectId/volumes
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "db-data", "sizeMB": 10240}
```
- Response (200)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "pending_name": null,
  "sizeMB": 10240,
  "pending_sizeMB": null,
  "status": "active"
}
```
- Note: volume は project に属する永続ストレージ。deploy を削除してもデータは残り、別の deploy から再マウントできる
- Note: 作成直後は name/sizeMB が指定値で既に反映済み(status: "active")。作成時点ではpendingは発生しない

### ボリューム一覧
- Get: /projects/:projectId/volumes
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "volumes": [
    {
      "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "name": "db-data",
      "pending_name": null,
      "sizeMB": 10240,
      "pending_sizeMB": null,
      "status": "active"
    }
  ]
}
```

### ボリューム取得
- Get: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "pending_name": null,
  "sizeMB": 10240,
  "pending_sizeMB": null,
  "status": "active",
  "mountedBy": ["deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"]
}
```
- Response (200、pending が存在する場合)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "pending_name": null,
  "sizeMB": 10240,
  "pending_sizeMB": 20480,
  "status": "active",
  "mountedBy": ["deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"]
}
```
- Note: name/sizeMB は現在有効な内容。pending_name/pending_sizeMB は[ボリューム更新](#ボリューム更新-一括置き換えpendingとして保存)後まだ[適用](#ボリューム-適用)されていない変更内容(変更が無ければ null)
- Note: status は "applying" | "active" | "failed" のいずれか。applyを呼ぶと反映が完了するまで "applying" になる

### ボリューム更新 (一括置き換え、pendingとして保存)
- Patch: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "db-data", "sizeMB": 20480}
```
- Response (200)
```json
{}
```
- Note: name/sizeMB は個別key単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)
- Note: この時点ではまだ反映されない。pending_name/pending_sizeMB として保存され、[ボリューム適用](#ボリューム-適用) を呼ぶまで name/sizeMB(現在有効な値)は変わらない
- Note: sizeMB は拡大のみ許可し、縮小(現在のsizeMBを下回る値)を指定した場合はエラーを返す(永続ボリュームの一般的な制約に合わせる)
- Note: volume の status が "applying" の間はこの操作を受け付けず 409 を返す

### ボリューム 適用
- Post: /projects/:projectId/volumes/:volumeId/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_name/pending_sizeMB の内容を name/sizeMB に反映する処理(ストレージのリサイズ等)を開始する。呼び出し自体は即座に応答するが、この時点ではまだ name/sizeMB は更新されていない。pending_name/pending_sizeMB が両方 null の場合は 409 を返す
- Note: 呼び出し直後、volume の status は "applying" になる。この間 pending_name/pending_sizeMB はまだ消費されず、適用しようとしている内容を保持したままとなる。反映の結果が確定した時点(成功・失敗いずれの場合も)で name/sizeMB が pending_name/pending_sizeMB の内容に更新され、pending_name/pending_sizeMB は null に戻る。成功すれば status は "active" に、失敗すれば "failed" になる
- Note: このapplyは volume をマウントしている deploy のコンテナ再起動を伴わない(ストレージ側の変更のみ)
- Note: 既に volume の status が "applying" の場合は 409 を返す(多重apply不可)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に反映完了が確認できない場合、強制的に status を "failed" に遷移させる

### ボリューム 破棄
- Post: /projects/:projectId/volumes/:volumeId/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_name/pending_sizeMB の内容を適用せずに破棄する(name/sizeMB には影響しない)。両方 null の場合は 409 を返す
- Note: volume の status が "applying" の間は 409 を返す

### ボリューム削除
- Delete: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: いずれかの deploy にマウント中の場合は 409 を返す(マウント解除してから削除する)

### デプロイ ボリュームマウント取得
- Get: /deploy/:deployId/mounts
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "mounts": [{"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"}],
  "pending_mounts": null
}
```
- Response (200、pending が存在する場合)
```json
{
  "mounts": [{"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"}],
  "pending_mounts": [
    {"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"},
    {"volumeId": "volume-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "mountPath": "/logs"}
  ]
}
```
- Note: mounts は現在コンテナに反映されているマウント構成。pending_mounts は未 /apply の変更内容(変更が無ければ null)

### デプロイへのボリュームマウント設定 (pendingとして保存)
- Put: /deploy/:deployId/mounts
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"mounts": [{"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"}]}
```
- Response (200)
```json
{}
```
- Note: 同一 project 内の volume のみマウント可能。config(env/command/args)と同様、常に全件を送って置き換える方式
- Note: この時点ではまだコンテナには反映されない。pending_mounts として保存され、/apply を呼ぶまで mounts(現在反映されている値)は変わらない
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はvolumesに対する変更・apply・discardを排他制御する)

### デプロイ ボリュームマウント 適用
- Post: /deploy/:deployId/mounts/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_mounts の内容を mounts に反映するため、コンテナの再起動を開始する。呼び出し自体は即座に応答するが、この時点ではまだ mounts は更新されていない。pending_mounts が無い場合は 409 を返す
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_mounts はまだ消費されず、適用しようとしている内容を保持したままとなる。再起動の結果が確定した時点(成功・失敗いずれの場合も)で mounts が pending_mounts の内容に更新され、pending_mounts は null に戻る。成功すれば status は "active" に、失敗すれば status は "failed" になる(mounts の内容自体は失敗時も新しい値で上書きされる。実際に起動できているかは status で判断する)([デプロイ詳細取得](deploys.md#デプロイ詳細取得) でポーリングして確認する)
- Note: 既に status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に status を "failed" に遷移させる

### デプロイ ボリュームマウント 破棄
- Post: /deploy/:deployId/mounts/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_mounts の内容を適用せずに破棄する(mounts には影響しない)。pending_mounts が無い場合は 409 を返す
- Note: deploy の status が "applying" の間は 409 を返す

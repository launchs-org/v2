[← エンドポイント一覧](README.md)

## ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/projects/:projectId/volumes` など)。
### ボリューム作成
- Post: /projects/:projectId/volumes
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "db-data", "sizeMB": 5120}
```
- Response (200)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "status": "active",
  "pending_sizeMB": null,
  "sizeMB": 5120
}
```
- Note: volume は project に属する永続ストレージ。deploy を削除してもデータは残り、別の deploy から再マウントできる
- Note: sizeMB には上限(デフォルト 5GB = 5120MB。環境変数 VOLUME_MAX_SIZE_MB)があり、超過した場合は 400 になる。現在の上限は volume 1つあたりで、アカウントごとの個別設定は、認証のコンテナを作るときに設ける。上限はアカウントごとに個別設定可能で、デフォルトを超える上限が必要な場合は申請して引き上げる(申請の手段・承認フローは別途、アカウント管理側で設計する)
- Note: status はボリューム自体の状態。"applying"(拡張の apply 中)| "active" | "failed"(拡張の反映に失敗)| "deleting"(削除中)。作成直後は "active"
- Note: name は英数字とハイフンのみ、最大64文字(最小1文字(仮))。違反は 400。project 内で重複した場合は 409
- Note: volume の実体は、環境変数(VOLUME_STORAGE_CLASS、空ならクラスターの既定)で選んだ StorageClass の PVC。拡張(リサイズ)に対応した StorageClass が前提。PVC は、作成の時点では作らず、最初にマウントを適用するとき(namespace がある状態)に、volume の現在の sizeMB で作る([docs/volume.md](../volume.md))
- Note: project の status が "deleting" の間は volume を作成できず 409 を返す

### ボリューム一覧
- Get: /projects/:projectId/volumes
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
  "volumes": [
    {
      "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "name": "db-data",
      "status": "active",
      "pending_sizeMB": null,
      "sizeMB": 5120
    }
  ],
  "nextCursor": null
}
```
- Note: 並び順は作成日時の降順。`limit` は1〜100(デフォルト20)、範囲外の `limit` と不正な `cursor` は 400(仮)。`nextCursor` は最後のページなら null、続きがあれば文字列。`cursor` はクライアントが解釈しない文字列として扱う
- Note: status はボリューム自体の状態。"applying"(拡張の apply 中)| "active" | "failed"(拡張の反映に失敗)| "deleting"(削除中)。作成直後は "active"

### ボリューム取得
- Get: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "status": "active",
  "pending_sizeMB": null,
  "sizeMB": 5120,
  "mountedBy": ["deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"]
}
```
- Note: sizeMB は現在有効な内容。pending_sizeMB は[ボリューム拡張](#ボリューム拡張-一括置き換えpendingとして保存)後まだ[適用](#ボリューム-適用)されていない変更内容(変更が無ければ null)
- Note: status はボリューム自体の状態。"applying"(拡張の apply 中)| "active" | "failed"(拡張の反映に失敗)| "deleting"(削除中)。作成直後は "active"

### ボリューム名前変更
- Patch: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "db-data-renamed"}
```
- Response (200)
```json
{}
```
- Note: name のみ変更可能(sizeMBはこのエンドポイントでは変更できない。拡張は[ボリューム拡張](#ボリューム拡張-一括置き換えpendingとして保存)を使う)。pending_*→apply/discardモデルは使わず、呼び出し直後に即時反映される(ストレージ側の実体には影響しないメタデータの変更のため)
- Note: name は英数字とハイフンのみ、最大64文字(最小1文字(仮))。違反は 400。project 内で他の volume と重複する場合は 409
- Note: volume の status が "deleting" の間はこの操作を受け付けず 409 を返す

### ボリューム拡張 (一括置き換え、pendingとして保存)
- Patch: /projects/:projectId/volumes/:volumeId/size
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"sizeMB": 10240}
```
- Response (200)
```json
{}
```
- Note: sizeMB は拡大のみ許可し、縮小(現在のsizeMBを下回る値)を指定した場合はエラーを返す(永続ボリュームの一般的な制約に合わせる)
- Note: この時点ではまだ反映されない。pending_sizeMB として保存され、[ボリューム適用](#ボリューム-適用) を呼ぶまで sizeMB(現在有効な値)は変わらない
- Note: アカウントごとの上限(デフォルト 5120MB)を超える sizeMB を指定した場合はエラーを返す
- Note: volume の status が "applying" の間はこの操作を受け付けず 409 を返す。"deleting" の間も 409 を返す
- Note: 拡張(リサイズ)は、環境変数で選んだ StorageClass の PVC を拡張して行う。拡張に対応した StorageClass が前提

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
- Note: pending_sizeMB の内容を sizeMB に反映する処理(ストレージのリサイズ)を開始する。呼び出し自体は即座に応答するが、この時点ではまだ sizeMB は更新されていない。pending_sizeMB が null の場合は 409 を返す
- Note: 呼び出し直後、volume の status は "applying" になる。この間 pending_sizeMB はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_sizeMB の値を使って先にストレージ側への反映を試み、その結果(成功・失敗)が確定した時点で初めて sizeMB を pending_sizeMB の内容に更新し、pending_sizeMB は null に戻す。成功すれば status は "active" に、失敗すれば "failed" になる。失敗したときは、sizeMB は動いている値のまま変わらず、pending_sizeMB は試した値として残る(PVC は縮められないので、巻き戻しはしない。同じ値で再度 apply しても問題ない)。failed でも、拡張の保存・apply・discard・削除ができる
- Note: このapplyは volume をマウントしている deploy のコンテナ再起動を伴わない(ストレージ側の変更のみ)
- Note: 既に volume の status が "applying" の場合は 409 を返す(多重apply不可)。"deleting" の間も 409 を返す
- Note: PVC を拡張し、PVC の実際の容量(status.capacity)が新しい sizeMB になるのを待つ。300秒(5分) 以内に増えなければ、status を "failed" にする。PVC がまだ無い(一度もマウントされていない)ときは、何もせず成功する(最初のマウントのとき、新しい sizeMB で作られる)

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
- Note: pending_sizeMB の内容を適用せずに破棄する(sizeMB には影響しない)。pending_sizeMB が null の場合は 409 を返す
- Note: volume の status が "applying" の間は 409 を返す。"deleting" の間も 409 を返す

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
- Note: 呼び出すと即座に応答し、status は直ちに "deleting" になる。実体の削除は Workflow で非同期に行い、完了するとレコード自体が削除される(以後の GET は 404 を返す)
- Note: deleting の間は、変更系の操作と再度の削除は 409 を返す。GET は status が "deleting" の状態を返す
- Note: いずれかの deploy にマウント中の場合は 409 を返す(マウント解除してから削除する)。まだ適用していない(pending_mounts にある)マウントも、マウント中として数える(適用の途中で PVC が消えるのを防ぐため)
- Note: volume の status が "applying" の間は 409 を返す(進行中の拡張applyと排他制御する)
- Note: project を削除すると、配下の volume は、マウントの有無にかかわらず、データごと削除される

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
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はvolumesに対する変更・apply・discardを排他制御する)。"deleting" の間も 409 を返す

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
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_mounts はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_mounts の値を使って先に Kubernetes 側への反映(コンテナ再起動)を試み、その結果(成功・失敗)が確定した時点で初めて mounts を pending_mounts の内容に更新し、pending_mounts は null に戻す。成功すれば status は "active" に、失敗すれば status は "failed" になる。失敗したときは、Deployment を元の mounts に戻し、mounts は動いている値のまま変わらず、pending_mounts は試した値として残る([デプロイ詳細取得](deploys.md#デプロイ詳細取得) でポーリングして確認する)
- Note: deploy の replicas が 0(status が "scaled_zero")のときは、再起動する Pod が無いのですぐに完了し、成功しても status は "active" ではなく "scaled_zero" に戻る。失敗は "failed" になる
- Note: deploy の status が "deleting" の間は 409 を返す
- Note: path は "/" で始まり、英数字と . _ - / だけの200文字以内で、"/" だけや ".." を含まない。同じ mountPath や volumeId は重複できない。mounts は10件まで。空の配列は、全て外す変更になる。保存の時点で、volume が同じ project にあり、削除中でないことを確かめ、不正なら 400
- Note: pending_mounts に含まれる volumeId の妥当性(同一project内のvolumeであること、削除中でないこと)は、apply実行時にも改めて検証する(不正なら 409)。マウント設定から適用までの間にvolume側の状態が変更される可能性があるため
- Note: 既に status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、失敗として、Deployment を元の mounts に戻し、status を "failed" にする
- Note: マウントを持つ deploy は、更新のとき、古い Pod を止めてから新しい Pod を起動する(Recreate)。アクセスモードが ReadWriteOnce(VOLUME_ACCESS_MODE で選んだ場合)の volume は、別のノードの新しい Pod が、古い Pod が持ったままでは使えないため。既定の ReadWriteMany でも、PVC のアクセスモードの組み合わせによらず安全なように、同じ方式にしている。そのぶん、マウントの適用の間、短い停止がある。アクセスモードは、PVC の作成のときの値で決まり、あとから変えられない。ReadWriteOnce の volume は、replicas を 2 以上にして、別のノードに置くと、2つ目の Pod が起動できない
- Note: PVC は、このapplyで、無ければ作る。volume を、複数の deploy にマウントすることもできる(同じ volume を、同じ deploy に2回は不可)

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
- Note: deploy の status が "applying" の間は 409 を返す。"deleting" の間も 409 を返す

[← エンドポイント一覧](README.md)

## デプロイ・起動設定関連 (/deploy)
### デプロイ (イメージから起動)
- Post: /deploy
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJhY2Nlc3MifQ.EO1DFWzMQfJJf66tm897rDFMN2GbeSPT8-Q1jw5wE1HgNaeD_rgctCJ5J2ZHSTPo4Ar7KzcH2fdyLEawkCC9Hw"
}
```
- Body
```json
{
  "projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx"
}
```
- Response (200)
```json
{"deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
```
- Note: sourceType(asset/git)には関知せず、常に imageRef のみを受け取ってデプロイする。詳細は未設計(別途)
- Note: deploy も projectId に属する。指定した projectId 配下の履歴として記録される

### デプロイ詳細取得
- Get: /deploy/:deployId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "status": "active",
  "replicas": 1,
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
  "createdAt": "2026-09-16T00:00:00Z",
  "config": {
    "env": {"PORT": "8080", "DATABASE_URL": "postgres://..."},
    "pending_env": null,
    "command": "/app/server",
    "pending_command": null,
    "args": ["--config", "/app/config.yaml"],
    "pending_args": null
  },
  "volumes": {
    "mounts": [{"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"}],
    "pending_mounts": null
  },
  "service": {
    "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "ports": [
      {"name": "web", "port": 8080, "protocol": "tcp"}
    ],
    "pending_ports": null,
    "status": "active",
    "dnsName": "web.svc.cluster.local",
    "clusterIP": "10.43.12.7"
  }
}
```
- Response (200、pending が存在する場合の抜粋)
```json
{
  "config": {
    "env": {"PORT": "8080"},
    "pending_env": {"PORT": "8081"},
    "command": "/app/server",
    "pending_command": null,
    "args": ["--config", "/app/config.yaml"],
    "pending_args": null
  },
  "volumes": {
    "mounts": [{"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"}],
    "pending_mounts": [
      {"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"},
      {"volumeId": "volume-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "mountPath": "/logs"}
    ]
  },
  "service": {
    "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "ports": [
      {"name": "web", "port": 8080, "protocol": "tcp"}
    ],
    "pending_ports": [
      {"name": "web", "port": 8080, "protocol": "tcp"},
      {"name": "game", "port": 27015, "protocol": "udp"}
    ],
    "status": "active",
    "dnsName": "web.svc.cluster.local",
    "clusterIP": "10.43.12.7"
  }
}
```
- Note: status は "pending" | "active" | "applying" | "failed" | "scaled_zero" のいずれか。"停止"という状態はなく、replicas を 0 にスケールすることで表現する
- Note: "起動指示が完了したか"はこの status で確認する(別途ヘルスチェック専用endpointは用意しない)
- Note: config/volumes/service いずれかの apply を呼ぶと、コンテナの再起動が完了するまで status は "applying" になる。再起動が成功すれば "active" に、失敗すれば "failed" に遷移する(applyの呼び出し自体は即時応答するが、反映の完了はこの status のポーリングで確認する)
- Note: applying→running/failed の判定は、バックエンドが Kubernetes の Deployment/Pod の conditions(Progressing, Ready など)を監視して行う(実装詳細は別途)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に status を "failed" に遷移させる(再起動処理がハングしても "applying" のまま無期限に固まらないようにするため)
- Note: config は [起動設定取得](#デプロイ-起動設定取得-環境変数起動コマンド引数)、volumes は [ボリュームマウント取得](volumes.md#デプロイ-ボリュームマウント取得)、service は [サービス取得](services.md#サービス取得) と同じ内容をこのレスポンスに埋め込んだもの。個別に叩かなくても、この1回のGETで全ての pending_* を確認できる
- Note: service はまだ作成されていない(サービス未作成の)deployでは null になる(1 deploy = 1 service で、作成は任意)

### デプロイ スケーリング
- Post: /deploy/:deployId/scale
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"replicas": 0}
```
- Response (200)
```json
{"deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "replicas": 0}
```
- Note: replicas を 0 にすると status は "scaled_zero" になる。1以上にすると "active" に戻る("停止"/"再開"という別APIは用意しない)

### デプロイ削除
- Delete: /deploy/:deployId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: replicas や status に関わらず、稼働中の deploy に対してもそのまま削除を受け付ける。事前にスケーリングで replicas を 0 にする必要はなく、削除処理の内部でコンテナの停止まで行う

### デプロイ 起動設定取得 (環境変数・起動コマンド・引数)
- Get: /deploy/:deployId/config
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、デプロイ作成直後の初期状態)
```json
{
  "env": {},
  "pending_env": null,
  "command": null,
  "pending_command": null,
  "args": null,
  "pending_args": null
}
```
- Response (200、起動設定を1度適用した後、pending が無い場合)
```json
{
  "env": {"PORT": "8080", "DATABASE_URL": "postgres://..."},
  "pending_env": null,
  "command": "/app/server",
  "pending_command": null,
  "args": ["--config", "/app/config.yaml"],
  "pending_args": null
}
```
- Response (200、pending が存在する場合)
```json
{
  "env": {"PORT": "8080", "DATABASE_URL": "postgres://..."},
  "pending_env": {"PORT": "8081", "DATABASE_URL": "postgres://..."},
  "command": "/app/server",
  "pending_command": null,
  "args": ["--config", "/app/config.yaml"],
  "pending_args": ["--config", "/app/config.v2.yaml"]
}
```
- Note: env/command/args は現在コンテナに反映されている値。pending_env/pending_command/pending_args はそれぞれ /config に PUT した後、まだ /apply されていない変更内容(変更が無ければ null)
- Note: command/args が null の場合、コンテナイメージ自身が持つデフォルトの起動コマンド(DockerfileのCMD/ENTRYPOINT等)がそのまま使われる。デプロイ作成直後は command/args ともに null であり、起動設定更新/適用を一度も行っていなければデフォルト起動コマンドのまま稼働する
- Note: env/command/args は常に3つセットで一括更新するため(部分更新はしない)、いずれか1つでも変更があれば pending_env/pending_command/pending_args の全てに値が入る(変更していないフィールドも直前にPUTした内容がそのまま入る)

### デプロイ 起動設定更新 (一括置き換え、pendingとして保存)
- Put: /deploy/:deployId/config
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "env": {"PORT": "8080", "DATABASE_URL": "postgres://..."},
  "command": "/app/server",
  "args": ["--config", "/app/config.yaml"]
}
```
- Response (200)
```json
{}
```
- Note: env/command/args は個別key単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)
- Note: この時点ではまだコンテナには反映されない。pending_env/pending_command/pending_args として保存され、/apply を呼ぶまで env/command/args(現在反映されている値)は変わらない
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はconfigに対する変更・apply・discardを排他制御する)

### デプロイ 起動設定 適用
- Post: /deploy/:deployId/config/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_env/pending_command/pending_args の内容を env/command/args に反映するため、コンテナの再起動を開始する。呼び出し自体は即座に応答するが、この時点ではまだ env/command/args は更新されていない。pending_env/pending_command/pending_args が全て null(何も更新していない)の場合は 409 を返す
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_env/pending_command/pending_args はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_* の値を使って先に Kubernetes 側への反映(コンテナ再起動)を試み、その結果(成功・失敗)が確定した時点で初めて env/command/args を pending_* の内容に更新し、pending_env/pending_command/pending_args は null に戻す。成功すれば status は "active" に、失敗すれば status は "failed" になる(env/command/args の内容自体は失敗時も新しい値で上書きされる。実際に起動できているかは status で判断する)([デプロイ詳細取得](#デプロイ詳細取得) でポーリングして確認する)
- Note: 既に status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に status を "failed" に遷移させる(再起動処理がハングしても "applying" のまま無期限に固まらないようにするため)

### デプロイ 起動設定 破棄
- Post: /deploy/:deployId/config/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_env/pending_command/pending_args の内容を適用せずに破棄する(env/command/args には影響しない)。全て null の場合は 409 を返す
- Note: deploy の status が "applying" の間は 409 を返す(applying中はconfigに対する操作を排他制御する)

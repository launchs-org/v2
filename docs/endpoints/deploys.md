[← エンドポイント一覧](README.md)

## デプロイ・起動設定関連 (/deploy)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/deploy` など)。

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
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
  "spec": "small"
}
```
- Response (200)
```json
{"deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
```
- Note: sourceType(asset/git)には関知せず、常に imageRef のみを受け取ってデプロイする。詳細は未設計(別途)
- Note: deploy も projectId に属する。指定した projectId 配下の履歴として記録される
- Note: spec は "small" | "medium" | "large" のいずれかで必須。省略した場合は 400(仮)。スペックはアカウント管理グループで定義するマスターデータ(CPU・メモリ)に対応する。アカウントのクォータを超える場合はエラーになる(ステータスコードはアカウント管理グループで定義する)。作成後のスペック変更は未対応(将来対応)
- Note: deleting 中のプロジェクトに deploy を作成しようとした場合は 409 を返す

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
  "spec": "small",
  "replicas": 1,
  "pods": [
    {"podId": "pod-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "status": "running"}
  ],
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
  "createdAt": "2026-09-16T09:00:00+09:00",
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
- Note: status は "pending" | "active" | "applying" | "failed" | "scaled_zero" | "deleting" のいずれか。"停止"という状態はなく、replicas を 0 にスケールすることで表現する
- Note: "起動指示が完了したか"はこの status で確認する(別途ヘルスチェック専用endpointは用意しない)
- Note: config/volumes/service いずれかの apply を呼ぶと、コンテナの再起動が完了するまで status は "applying" になる。再起動が成功すれば "active" に、失敗すれば "failed" に遷移する(applyの呼び出し自体は即時応答するが、反映の完了はこの status のポーリングで確認する)
- Note: applying→running/failed の判定は、バックエンドが Kubernetes の Deployment/Pod の conditions(Progressing, Ready など)を監視して行う(実装詳細は別途)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に status を "failed" に遷移させる(再起動処理がハングしても "applying" のまま無期限に固まらないようにするため)
- Note: config は [起動設定取得](#デプロイ-起動設定取得-環境変数起動コマンド引数)、volumes は [ボリュームマウント取得](volumes.md#デプロイ-ボリュームマウント取得)、service は [サービス取得](services.md#サービス取得) と同じ内容をこのレスポンスに埋め込んだもの。個別に叩かなくても、この1回のGETで全ての pending_* を確認できる
- Note: service の status(pending / active / failed / deleting)は Service 自体の払い出し状態で、deploy の status とは別。config/volumes/service いずれかの apply 中に変化するのは deploy の status だけで、service の status は変わらない
- Note: service はまだ作成されていない(サービス未作成の)deployでは null になる(1 deploy = 1 service で、作成は任意)
- Note: pods は現在稼働している Pod の一覧(replicas が希望レプリカ数という数値であるのに対し、pods は実際の Pod 1つ1つの識別子と状態を返す)。[デプロイ ログ取得](#デプロイ-ログ取得) で特定の Pod のログを取得する際に、ここで取得した podId を指定する。status は "pending" | "running" | "terminating" のいずれか。status が "pending" の場合 deploy の status が "applying"(起動中)であることが多い

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
- Note: deploy の status が "applying" または "deleting" の間は 409 を返す(他の apply 操作と同様に排他制御する)
- Note: replicas は 0〜10(上限 10 は仮)。範囲外や未指定は 400
- Note: 呼び出すと replicas を新しい値にして、status は "applying" になり、反映(Pod の増減)の完了は status のポーリングで確認する。完了すると "active"(0 のときは "scaled_zero")になり、300秒で完了しなければ "failed" になる。replicas は「希望する数」なので、失敗しても元には戻さない。"failed" の deploy は、同じ数でもスケーリングし直せる
- Note: 今と同じ replicas(failed 以外)なら、何も変えずに 200 を返す。起動中("pending")の間と "delete_failed" の間は 409 を返す
- Note: 未適用の起動設定(pending_*)は、スケーリングでは反映されない

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
- Note: 呼び出すと即座に応答し、status は直ちに "deleting" になる。実体の削除は Workflow で非同期に行い、完了するとレコード自体が削除される(以後の GET は 404 を返す)
- Note: deleting の間は、変更系の操作と再度の削除は 409 を返す。GET は status が "deleting" の状態を返す
- Note: deploy の status が "applying" の間は 409 を返す(進行中の apply を中断して削除することはしない)
- Note: 配下の service も連鎖して削除される(service は "deleting" になる)。ボリュームのマウント設定も削除されるが、マウントされていた volume 自体は残る(volume は project に属するため)
- Note: deploy のログも deploy の削除と一緒に削除される
- Note: 起動中("pending")の間も 409 を返す。削除に失敗した("delete_failed")deploy は、もう一度削除を呼んでやり直せる(リトライの期限は 15 分で、超えると "delete_failed" になる)

### デプロイ ログ取得
- Get: /deploy/:deployId/logs
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query (必須: podId、任意: since)
```json
{"podId": "pod-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "since": 128}
```
- Response (200)
```json
{"logs": "2026-09-16T09:00:01+09:00 [INFO] server started on :8080\n...", "nextSince": 256}
```
- Note: ストリーミングではなくポーリング前提。[ビルドログ取得](builds.md#ビルドログ取得)と同じ方式で、since を省略すると先頭からの全ログ、直前のレスポンスの nextSince を since に渡すと差分のみ返る
- Note: podId は必須。[デプロイ詳細取得](#デプロイ詳細取得) の pods 配列から取得した podId を指定し、その Pod 1つのログのみを返す(replicas が複数ある場合、全replica分をまとめて返す機能は無い。複数replicaのログを見たい場合はpodIdを変えて複数回呼び出す)
- Note: ログは収集コンテナが Pod のログを購読して DB に保存する。保持期間は7日で、Pod ごとに上限(50MB)があり、超えたら古い行から削除される
- Note: 保持期間内なら、既に終了・再作成された Pod の podId でもログを取得できる。podId が、その deploy に属したことがない、またはログが保持期間を過ぎて削除済みの場合は 404 を返す。pods 配列は現在稼働している Pod だけを返す(終了した Pod は含まない)
- Note: ログは DB に書き込まれた分を返すので、Pod の出力から少し遅れる(収集コンテナの切り替え中は遅れが大きくなりうる)(仮)
- Note: 削除した deploy のログは、deploy の削除と一緒に削除される
- Note: since に現在のログ末尾を超える値を指定した場合はエラーにせず、logs は空文字列、nextSince は現在のログ末尾の位置を返す

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
- Note: 送った内容が現在の env/command/args と同じ(変更が無い)場合は、pending_* を保存せず null にする(以前の pending_* も消える)。そのため、同じ設定を apply し直すことはできない(409)
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はconfigに対する変更・apply・discardを排他制御する)。"deleting" の間も 409 を返す

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
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_env/pending_command/pending_args はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_* の値を使って先に Kubernetes 側への反映(コンテナ再起動)を試み、その結果が確定した時点で、成功なら env/command/args を pending_* の内容に更新して pending_* を null に戻す。成功すれば status は "active" に、失敗すれば status は "failed" になる。env/command/args は常に「いま動いている値」を表すので、成功したときだけ pending_* の内容に更新し、pending_* を null に戻す。失敗したときは、Kubernetes の Deployment を元の値(env/command/args)に戻した上で、env/command/args も pending_* も変えず(試して失敗した値は pending_* に残る)、status だけを "failed" にする。利用者は pending_* を直して PUT し直すか、そのまま再度 apply するか、discard で破棄できる([デプロイ詳細取得](#デプロイ詳細取得) でポーリングして確認する)
- Note: 既に status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)。"deleting" の間も 409 を返す
- Note: deploy の replicas が 0(status が "scaled_zero")のときは、再起動する Pod が無いのですぐに完了し、成功しても status は "active" ではなく "scaled_zero" に戻る。
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
- Note: deploy の status が "applying" の間は 409 を返す(applying中はconfigに対する操作を排他制御する)。"deleting" の間も 409 を返す

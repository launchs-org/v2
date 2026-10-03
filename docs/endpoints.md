# エンドポイント一覧

## 概要
全てのエンドポイントをまとめる

## 認証関連 (/auth/)
### サインアップ
- Post: /signup
- Header
```json
{"Content-Type": "application/json"}
```
- Body
```json
{"userName" : "user1","Password1"}
```
- Response (200)
```json
{
  "token": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```

### ログイン 
- Post: /login
- Header
```json
{"Content-Type": "application/json"}
```
- Body
```json
{"userName" : "user1","Password1"}
```
- Response (200)
```json
{
  "token": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```

### アクセストークン (取得)
- Post: /refresh
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Response (200)
```json
{
  "token": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJhY2Nlc3MifQ.EO1DFWzMQfJJf66tm897rDFMN2GbeSPT8-Q1jw5wE1HgNaeD_rgctCJ5J2ZHSTPo4Ar7KzcH2fdyLEawkCC9Hw"
}
```

### 自分の情報取得
- Get: /me
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"userId": "userid-80b0b29c-7514-40dd-a0b4-8f22f09329d8", "userName": "user1"}
```

### ログアウト (/logout)
- Post: /logout
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Response (200)
```json
{}
```

## プロジェクト関連 (/projects)
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

### プロジェクト一覧
- Get: /projects
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projects": [{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app"}]}
```

### プロジェクト取得
- Get: /projects/:projectId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app"}
```

### プロジェクト build 履歴一覧
- Get: /projects/:projectId/builds
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "builds": [
    {
      "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "success",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T00:00:00Z"
    }
  ]
}
```
- Note: 作成日時の降順で返す(ページングは未設計・別途)

### プロジェクト deploy 履歴一覧
- Get: /projects/:projectId/deploys
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "deploys": [
    {
      "deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "active",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T00:00:00Z"
    }
  ]
}
```
- Note: 作成日時の降順で返す(削除済みdeployを含めるかは未設計・別途)。ページングは未設計・別途

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
- Note: プロジェクト削除時、配下の build 履歴・イメージ・deploy(稼働中のものを含む)は全て連鎖して削除される

## デプロイ関連 (/deoloy)
### アップロード
- Post: /upload
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Request
- zip でボディを送信する (MAX100MB)
- Response (200)
- Note: 30分間有効なAssetトークンを吐き出す これをビルドに渡してダウンロードを行う
```json
{
  "assetsToken": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsImFzc2V0SWQiOiJhc3NldC0wMmVlYjFmOC0yODRlLTRiZGItODUxNi1jM2QzMzdlMzk0NjAiLCJzaGEyNTYiOiI5ODM0ODc2RENGQjA1Q0IxNjdBNUMyNDk1M0VCQTU4QzRBQzg5QjFBREY1N0YyOEYyRjlEMDlBRjEwN0VFOEYwIiwidHlwZSI6ImFzc2V0In0.6YQ16LRR0E5JcTIFdFrBcsttm6ki0HWmgmju5irGo9XJWkcCEXqD7MXmVQ7II_Y2ydA29yCwp96VIvX2k78_3g"
}
```

### ビルド (イメージ作成)
- Post: /build
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJhY2Nlc3MifQ.EO1DFWzMQfJJf66tm897rDFMN2GbeSPT8-Q1jw5wE1HgNaeD_rgctCJ5J2ZHSTPo4Ar7KzcH2fdyLEawkCC9Hw"
}
```
- Body (asset の場合)
```json
{
  "projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "sourceType": "asset",
  "asset": {"assetsToken": "<upload発行のトークン>"},
  "buildTargetDirectory": "packages/api",
  "buildMethod": "dockerfile"
}
```
- Body (git の場合、public repoのみ対応)
```json
{
  "projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "sourceType": "git",
  "git": {"repoUrl": "https://github.com/user/repo.git", "ref": "main"},
  "buildTargetDirectory": "packages/api",
  "buildMethod": "railpack"
}
```
- Response (200)
```json
{
  "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx"
}
```
- Note: build は projectId に属する。指定した projectId 配下の履歴として記録される
- Note: ビルドは非同期で開始される。buildId は状況追跡用。imageRef はこの時点で確定・払い出され以後不変だが、実際にpullしてデプロイに使えるのは status が "success" になってから(ビルド状況取得は別endpointとして別途設計)
- Note: imageRef のタグ部分は buildId を使う(例: project-xxxx:build-xxxx)。"latest" のような固定タグは使わず、ビルドごとに一意なタグにすることで、後続のビルドが以前のイメージを上書きしないようにする
- Note: sourceType が git の場合、現時点では public リポジトリのみ対応(private認証は未対応・将来対応)
- Note: buildTargetDirectory は任意項目。sourceType(asset/git)によらず共通で、ソースツリー内のビルド対象ディレクトリを指定する(monorepo対応)。省略時はルート直下を対象とする
- Note: buildMethod は必須項目("dockerfile" | "railpack")。省略した場合は自動判定を行わず 400 を返す

### ビルド状態取得
- Get: /build/:buildId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "status": "running",
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx"
}
```
- Note: status は "pending" | "running" | "canceling" | "success" | "failed" | "canceled" のいずれか
- Note: imageRef は [ビルド作成](/build) の時点で確定し、以後不変(build成功を待たずに参照可能。ただし実際にpullできるのは status が "success" になった後)

### ビルドログ取得
- Get: /build/:buildId/logs
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query (任意)
```json
{"since": 128}
```
- Response (200)
```json
{"logs": "Step 1/5 : FROM golang:1.27\n...", "nextSince": 256}
```
- Note: ストリーミングではなくポーリング前提。since を省略すると先頭からの全ログ、直前のレスポンスの nextSince を since に渡すと差分のみ返る
- Note: status が "running" の間は繰り返しポーリングすることで最新ログを追える。status が "success" | "failed" | "canceled" になった後も同じ方式でログ全体を取得できる(failed で中断したビルドのログも取得可能)

### ビルドキャンセル
- Post: /build/:buildId/cancel
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: status が "pending" または "running" の場合のみ有効。呼び出すと即座に status は "canceling" になり、実際のビルドプロセス中断が完了すると "canceled" に遷移する(この時点ではまだ "canceled" になっていない)
- Note: 既に "canceling" | "success" | "failed" | "canceled" の場合は 409 を返す(多重キャンセル不可)

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
- Note: config は [起動設定取得](/deploy/:deployId/config)、volumes は [ボリュームマウント取得](/deploy/:deployId/mounts)、service は [サービス取得](/deploy/:deployId/service) と同じ内容をこのレスポンスに埋め込んだもの。個別に叩かなくても、この1回のGETで全ての pending_* を確認できる
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
- Note: 削除前に replicas を 0 にスケールしてから削除する(停止という概念はない)

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
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_env/pending_command/pending_args はまだ消費されず、適用しようとしている内容を保持したままとなる。再起動の結果が確定した時点(成功・失敗いずれの場合も)で env/command/args が pending_env/pending_command/pending_args の内容に更新され、pending_env/pending_command/pending_args は null に戻る。成功すれば status は "active" に、失敗すれば status は "failed" になる(env/command/args の内容自体は失敗時も新しい値で上書きされる。実際に起動できているかは status で判断する)([デプロイ詳細取得](/deploy/:deployId) でポーリングして確認する)
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

## サービス関連 (/deploy/:deployId/service)
### サービス作成 (ポート公開)
- Post: /deploy/:deployId/service
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ]
}
```
- Response (200)
```json
{
  "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ],
  "pending_ports": null,
  "status": "pending",
  "dnsName": null,
  "clusterIP": null
}
```
- Note: service は deploy に1つだけ属する(1 deploy = 1 service)。既にserviceが存在するdeployに対して再度呼び出すと 409 を返す
- Note: service はコンテナ内の複数ポートを、クラスタ内(および必要ならingressのpath ruleから参照されて外部)へ公開する単位。ports配列の各要素でport番号ごとにprotocol("tcp"|"udp")を選べる
- Note: HTTPという制約はここには無い(TCP/UDPどちらのポートも登録できる)。「HTTPのみ対応」の制約は[イングレス](/projects/:projectId/ingresses)側にあり、ingressのpath ruleはprotocol "tcp" のポートのみ参照できる
- Note: status は "pending" | "active" | "failed" | "deleting" のいずれか。dnsName("<name>.svc.cluster.local" 形式、nameはdeployごとに割り当てられるサービス名)と clusterIP は "active" になるまで null(status/dnsName/clusterIP はサービス自体の払い出し状態であり、pending_*の対象ではない)

### サービス取得
- Get: /deploy/:deployId/service
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ],
  "pending_ports": null,
  "status": "active",
  "dnsName": "web.svc.cluster.local",
  "clusterIP": "10.43.12.7"
}
```
- Response (200、pending が存在する場合)
```json
{
  "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "metrics", "port": 9090, "protocol": "tcp"}
  ],
  "pending_ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "game", "port": 27015, "protocol": "udp"}
  ],
  "status": "active",
  "dnsName": "web.svc.cluster.local",
  "clusterIP": "10.43.12.7"
}
```
- Note: ports は現在反映されている内容。pending_ports は更新リクエスト後まだ apply されていない変更内容(変更が無ければ null)
- Note: サービスが未作成のdeployに対して呼び出すと 404 を返す

### サービス更新 (一括置き換え、pendingとして保存)
- Patch: /deploy/:deployId/service
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "game", "port": 27015, "protocol": "udp"}
  ]
}
```
- Response (200)
```json
{}
```
- Note: ports は個別要素単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)。ポートの追加・削除・変更は全てこのエンドポイントで配列ごと送る
- Note: この時点ではまだ反映されない。pending_ports として保存され、サービスの apply を呼ぶまで ports(現在反映されている値)は変わらない
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はserviceに対する変更・apply・discardを排他制御する)

### サービス 適用
- Post: /deploy/:deployId/service/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_ports の内容を ports に反映するため、コンテナの再起動を開始する。呼び出し自体は即座に応答するが、この時点ではまだ ports は更新されていない。pending_ports が null の場合は 409 を返す
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 service の pending_ports はまだ消費されず、適用しようとしている内容を保持したままとなる。再起動の結果が確定した時点(成功・失敗いずれの場合も)で service の ports が pending_ports の内容に更新され、pending_ports は null に戻る。成功すれば deploy の status は "active" に、失敗すれば "failed" になる(service の ports の内容自体は失敗時も新しい値で上書きされる)([デプロイ詳細取得](/deploy/:deployId) でポーリングして確認する)
- Note: 既に deploy の status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に deploy の status を "failed" に遷移させる

### サービス 破棄
- Post: /deploy/:deployId/service/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_ports の内容を適用せずに破棄する(ports には影響しない)。pending_ports が null の場合は 409 を返す
- Note: deploy の status が "applying" の間は 409 を返す

### サービス削除
- Delete: /deploy/:deployId/service
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: 呼び出すと即座に応答し、service の status は直ちに "deleting" になる。実体(k8s Service等)の削除処理は非同期に開始され、完了するとレコード自体が削除される([サービス取得](/deploy/:deployId/service)は 404 を返すようになる)
- Note: 当該 service の各ポートを参照している ingress の path rule は、呼び出し時点(status: "deleting" になった時点)で連鎖して削除される
- Note: 既に status が "deleting" の間に再度削除を呼び出した場合は 409 を返す(削除処理中の多重実行不可)

## イングレス関連 (/projects/:projectId/ingresses)
### イングレス作成 (ランダムドメイン発行)
- Post: /projects/:projectId/ingresses
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [],
  "pending_rules": null,
  "status": "active"
}
```
- Note: ingress は project に属する(deployには属さない)。host はランダムなサブドメインが自動割り当てされる(host を指定して作成する形にはしない)
- Note: 対応プロトコルは HTTP のみ。TCP/UDP の公開は未対応(将来対応)
- Note: 作成直後は rules が空配列の状態で反映済み(status: "active")。path ruleを設定するには [イングレス更新](/projects/:projectId/ingresses/:ingressId) を使う

### イングレス一覧
- Get: /projects/:projectId/ingresses
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "ingresses": [
    {
      "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "host": "random-word-1234.launchs.org"
    }
  ]
}
```
- Note: 一覧では ingressId/host のみを返す。rules/pending_rules/status の詳細は[イングレス取得](/projects/:projectId/ingresses/:ingressId)で個別に確認する

### イングレス取得
- Get: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080}
  ],
  "pending_rules": null,
  "status": "active"
}
```
- Response (200、pending が存在する場合)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080}
  ],
  "pending_rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080},
    {"path": "/api", "serviceId": "service-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "port": 8081}
  ],
  "status": "active"
}
```
- Note: rules は現在反映されているpath ruleの一覧。pending_rules は[イングレス更新](/projects/:projectId/ingresses/:ingressId)後まだ[適用](/projects/:projectId/ingresses/:ingressId/apply)されていない変更内容(変更が無ければ null)
- Note: status は "applying" | "active" | "failed" のいずれか。applyを呼ぶと反映が完了するまで "applying" になる

### イングレス削除
- Delete: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: 配下の path rule(rules/pending_rules) も全て連鎖して削除される

### イングレス更新 (rules 一括置き換え、pendingとして保存)
- Put: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080},
    {"path": "/api", "serviceId": "service-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "port": 8081}
  ]
}
```
- Response (200)
```json
{}
```
- Note: rules は個別rule単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)。path ruleの追加・削除・変更は全てこのエンドポイントで配列ごと送る
- Note: この時点ではまだ反映されない。pending_rules として保存され、[イングレス適用](/projects/:projectId/ingresses/:ingressId/apply) を呼ぶまで rules(現在反映されている値)は変わらない
- Note: serviceId には同一 project 内の任意の deploy に属する service を指定できる。path ごとに向き先の service を切り替えられる
- Note: port には対象serviceの ports 配列に含まれるポート番号を指定する。ingressはHTTPのみ対応のため、指定するportは対象serviceのports配列で protocol が "tcp" のものに限る("udp"のポートを指定した場合はエラーになる)
- Note: ingress の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はrulesに対する変更・apply・discardを排他制御する)

### イングレス 適用
- Post: /projects/:projectId/ingresses/:ingressId/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_rules の内容を rules に反映するため、ルーティング設定の書き換えを開始する。呼び出し自体は即座に応答するが、この時点ではまだ rules は更新されていない。pending_rules が null の場合は 409 を返す
- Note: 呼び出し直後、ingress の status は "applying" になる。この間 pending_rules はまだ消費されず、適用しようとしている内容を保持したままとなる。反映の結果が確定した時点(成功・失敗いずれの場合も)で rules が pending_rules の内容に更新され、pending_rules は null に戻る。成功すれば status は "active" に、失敗すれば "failed" になる(rules の内容自体は失敗時も新しい値で上書きされる)
- Note: ingressの反映はdeployのコンテナ再起動を伴わないルーティング設定の書き換えのみのため、config/volumes/serviceのapplyより短時間で完了する想定だが、確認手順(status のポーリング)は同様
- Note: 既に ingress の status が "applying" の場合は 409 を返す(多重apply不可)
- Note: applying には 60秒 のタイムアウトを設ける。この時間内に反映完了が確認できない場合、強制的に status を "failed" に遷移させる

### イングレス 破棄
- Post: /projects/:projectId/ingresses/:ingressId/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_rules の内容を適用せずに破棄する(rules には影響しない)。pending_rules が null の場合は 409 を返す
- Note: ingress の status が "applying" の間は 409 を返す

## ボリューム関連 (/projects/:projectId/volumes)
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
- Note: name/sizeMB は現在有効な内容。pending_name/pending_sizeMB は[ボリューム更新](/projects/:projectId/volumes/:volumeId)後まだ[適用](/projects/:projectId/volumes/:volumeId/apply)されていない変更内容(変更が無ければ null)
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
- Note: この時点ではまだ反映されない。pending_name/pending_sizeMB として保存され、[ボリューム適用](/projects/:projectId/volumes/:volumeId/apply) を呼ぶまで name/sizeMB(現在有効な値)は変わらない
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
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_mounts はまだ消費されず、適用しようとしている内容を保持したままとなる。再起動の結果が確定した時点(成功・失敗いずれの場合も)で mounts が pending_mounts の内容に更新され、pending_mounts は null に戻る。成功すれば status は "active" に、失敗すれば status は "failed" になる(mounts の内容自体は失敗時も新しい値で上書きされる。実際に起動できているかは status で判断する)([デプロイ詳細取得](/deploy/:deployId) でポーリングして確認する)
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

[← デプロイ・起動設定関連](README.md)

# デプロイ詳細取得

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Get /deploy/:deployId`

## 説明

deploy の現在の状態(`status`/`replicas`/`pods`/`imageRef`)に加え、[起動設定](005-get-deploy-deployid-config.md)・[ボリュームマウント](../volumes/README.md)・[サービス](../services/README.md)の内容を1回のGETでまとめて取得できるエンドポイント。個別に叩かなくても、この1回で全ての `pending_*` を確認できる。

## テスト要件

### 正常系
- 作成直後は `status: "pending"` で、`config`/`volumes` はデフォルト値(`env`空オブジェクト、`command`/`args` null、`mounts`空配列)、`service` は `null` であること
- `config`/`volumes`/`service` いずれかの apply を呼ぶと `status` が `"applying"` になること
- 再起動が成功すると `status` が `"active"` に、失敗すると `"failed"` に遷移すること
- `pending_*` が存在する場合、`config`/`volumes`/`service` 配下にそれぞれの `pending_env`/`pending_mounts`/`pending_ports` 等が反映されていること
- サービス未作成のdeployでは `service` が `null` であること
- `replicas` が1以上の場合、`pods` 配列に実際に稼働している Pod の数だけ `podId`/`status` が含まれること
- `replicas: 0`(`scaled_zero`)の場合、`pods` が空配列になること
- [デプロイ ログ取得](009-get-deploy-deployid-logs.md) で使う `podId` は、この `pods` 配列から取得できること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId`(自分が所属していないプロジェクトのdeploy)を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `status` は "pending" | "active" | "applying" | "failed" | "scaled_zero" 以外の値を取らないこと
- `applying` が 300秒(5分)を超えても完了しない場合、強制的に `"failed"` に遷移すること
- `pods` 配列内の各 `status` は "pending" | "running" | "terminating" 以外の値を取らないこと

## 仕様

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
  "pods": [
    {"podId": "pod-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "status": "running"}
  ],
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
- Note: config は [起動設定取得](005-get-deploy-deployid-config.md)、volumes は [ボリュームマウント取得](../volumes/README.md)、service は [サービス取得](../services/README.md) と同じ内容をこのレスポンスに埋め込んだもの。個別に叩かなくても、この1回のGETで全ての pending_* を確認できる
- Note: service はまだ作成されていない(サービス未作成の)deployでは null になる(1 deploy = 1 service で、作成は任意)
- Note: pods は現在稼働している Pod の一覧(replicas が希望レプリカ数という数値であるのに対し、pods は実際の Pod 1つ1つの識別子と状態を返す)。[デプロイ ログ取得](009-get-deploy-deployid-logs.md) で特定の Pod のログを取得する際に、ここで取得した podId を指定する。status は "pending" | "running" | "terminating" のいずれか

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)

[← デプロイ・起動設定関連](README.md)

# デプロイ削除

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Delete /deploy/:deployId`

## 説明

deploy を削除するエンドポイント。`replicas` の値や `status` に関わらず、稼働中の deploy に対してもそのまま削除コマンドを受け付ける(事前にスケーリングで `replicas` を 0 にする手順は不要)。

## テスト要件

### 正常系
- `replicas` が1以上の稼働中(`status: "active"`)の deploy に対してそのまま削除を呼び出しても 200 が返り、コンテナの停止・削除まで一貫して行われること
- 削除後、[デプロイ詳細取得](002-get-deploy-deployid.md) が 404 になること
- 削除時、配下の [サービス](../services/README.md) も連鎖して削除されること
- 削除時、配下のボリュームマウント設定は削除されるが、マウントされていた [ボリューム](../volumes/README.md) 自体は残ること(volumeはprojectに属するため)

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `status` が `"applying"` の間に削除を呼び出した場合の挙動(進行中のapplyを中断して削除するか、409で拒否するか)は仕様未確定のため別途定義する

### 境界値
- 特になし

## 仕様

- Delete: /deploy/:deployId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: replicas や status に関わらず、稼働中の deploy に対してもそのまま削除を受け付ける。事前に [スケーリング](003-post-deploy-deployid-scale.md) で replicas を 0 にする必要はなく、削除処理の内部でコンテナの停止まで行う

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)

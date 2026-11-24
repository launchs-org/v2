[← ボリューム関連](README.md)

# デプロイへのボリュームマウント設定 (pendingとして保存)

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Put /deploy/:deployId/mounts`

## 説明

`mounts`(volume と mountPath の組の配列)を一括で置き換え、`pending_mounts` として保存するエンドポイント。[適用](011-post-deploy-deployid-mounts-apply.md) を呼ぶまで実際には反映されない。同一 project 内の volume のみマウント可能。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 同一 project 内の `volumeId` と `mountPath` を指定すると 200 が返り、[マウント取得](009-get-deploy-deployid-mounts.md) の `pending_mounts` に反映されること
- 呼び出し直後、`mounts`(現在反映されている値)は変化しないこと
- 複数の volume を1つの `mounts` 配列にまとめて指定できること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 他プロジェクトに属する `volumeId` を指定した場合エラーになること
- 存在しない `volumeId` を指定した場合エラーになること
- `status` が `"applying"` の間はこの操作を受け付けず 409 が返ること(applying中はvolumesに対する変更・apply・discardを排他制御する)
- deploy の `status` が `"deleting"` の間は 409 が返ること

### 境界値
- `mounts` は個別要素単位のCRUDではなく、常に全件を送って置き換える方式であること(部分更新は不可)
- 同一 `mounts` 配列内で `mountPath` が重複した場合 400 が返ること
- 同一 `mounts` 配列内で同じ `volumeId` を複数回指定した場合の扱い(許可するか400にするか)は仕様未確定のため別途定義する

## 仕様

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
- Note: この時点ではまだコンテナには反映されない。pending_mounts として保存され、[適用](011-post-deploy-deployid-mounts-apply.md) を呼ぶまで mounts(現在反映されている値)は変わらない
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はvolumesに対する変更・apply・discardを排他制御する)。"deleting" の間も 409 を返す

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

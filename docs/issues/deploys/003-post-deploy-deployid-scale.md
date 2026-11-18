[← デプロイ・起動設定関連](README.md)

# デプロイ スケーリング

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Post /deploy/:deployId/scale`

## 説明

deploy のレプリカ数を変更するエンドポイント。"停止"/"再開"という別APIは用意せず、`replicas` を 0 にすることで停止相当を表現する。 エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `replicas: 0` を指定すると 200 が返り、[デプロイ詳細取得](002-get-deploy-deployid.md) で `status` が `"scaled_zero"` になること
- `replicas` を1以上に指定すると `status` が `"active"` に戻ること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `replicas` が未指定、または負の値の場合 400 が返ること
- `status` が `"applying"` の間にスケーリングを呼び出した場合 409 が返ること
- `status` が `"deleting"` の間はスケーリングを受け付けず 409 が返ること

### 境界値
- `replicas` の上限値は 10(仮)。10 は成功し、11 は 400 になること
- 今と同じ `replicas` を指定した場合は、何も変えずに 200 が返ること
- `status` が `"applying"` または `"deleting"` の間は、スケーリングが 409 で拒否されること

## 仕様

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
- Note: replicas は 0〜5(全体で同じ上限。スペックやクォータとは関係ない。[クォータ](../../endpoints/quota.md)も参照)。範囲外や未指定は 400
- Note: 呼び出すと replicas を新しい値にして、status は "applying" になり、反映(Pod の増減)の完了は status のポーリングで確認する。完了すると "active"(0 のときは "scaled_zero")になり、300秒で完了しなければ "failed" になる。replicas は「希望する数」なので、失敗しても元には戻さない。"failed" の deploy は、同じ数でもスケーリングし直せる
- Note: 今と同じ replicas(failed 以外)なら、何も変えずに 200 を返す。起動中("pending")の間と "delete_failed" の間は 409 を返す
- Note: 未適用の起動設定(pending_*)は、スケーリングでは反映されない

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

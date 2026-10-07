[← デプロイ・起動設定関連](README.md)

# デプロイ スケーリング

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Post /deploy/:deployId/scale`

## 説明

deploy のレプリカ数を変更するエンドポイント。"停止"/"再開"という別APIは用意せず、`replicas` を 0 にすることで停止相当を表現する。

## テスト要件

### 正常系
- `replicas: 0` を指定すると 200 が返り、[デプロイ詳細取得](002-get-deploy-deployid.md) で `status` が `"scaled_zero"` になること
- `replicas` を1以上に指定すると `status` が `"active"` に戻ること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `replicas` が未指定、または負の値の場合 400 が返ること

### 境界値
- `replicas` の上限値(仕様未確定のため別途定義する)
- `status` が `"applying"` の間にスケーリングを呼び出した場合の挙動(他のapply操作と同様に409で排他制御するか、仕様未確定のため別途定義する)

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

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)

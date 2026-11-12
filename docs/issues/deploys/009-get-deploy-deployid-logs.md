[← デプロイ・起動設定関連](README.md)

# デプロイ ログ取得

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Get /deploy/:deployId/logs`

## 説明

deploy を構成する特定の Pod のランタイムログを取得するエンドポイント。[ビルドログ取得](../builds/004-get-build-buildid-logs.md) と同じ `since`/`nextSince` によるポーリング方式。障害発生時の切り分けを目的とする。ログは収集コンテナが Pod のログを購読して DB に保存したもので、保持期間は7日、Pod ごとに上限(50MB)があり、超えたら古い行から削除される。保持期間内なら、既に終了・再作成された Pod の `podId` でも取得できる。[デプロイ詳細取得](002-get-deploy-deployid.md) の `pods` 配列から取得した `podId` を指定して呼び出す。 エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 有効な `podId` を指定すると 200 が返り、`logs`/`nextSince` が取得できること
- `since` を省略すると先頭からの全ログが返ること
- 直前のレスポンスの `nextSince` を `since` に指定すると、その時点からの差分ログのみ返ること
- `replicas` が2以上で複数の `podId` が存在する場合、それぞれの `podId` を指定して個別にログを取得できること
- 保持期間内であれば、既に終了・再作成された Pod の `podId`(現在の `pods` に無いもの)でもログを取得できること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `podId` が未指定の場合 400 が返ること
- その deploy に属したことがない `podId` を指定した場合 404 が返ること(他の `deployId` に属する `podId` を含む)
- ログが保持期間(7日)を過ぎて削除済みの `podId` を指定した場合 404 が返ること

### 境界値
- `since` に現在のログ末尾を超える値を指定した場合、エラーにはせず `logs` は空文字列、`nextSince` は現在のログ末尾の位置を返すこと
- Pod が再起動されて新しい `podId` が割り当てられた後も、保持期間内は古い `podId` のログを取得できること。`pods` 配列は現在稼働している Pod だけを返し、終了した Pod は含まないこと
- Pod ごとのログが上限(50MB)を超えた場合、古い行から削除されること
- ログは DB に書き込まれた分を返すので、Pod の出力から少し遅れること(収集コンテナの切り替え中は遅れが大きくなりうる)(仮)
- 削除した deploy のログは、deploy の削除と一緒に削除されること

## 仕様

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
- Note: ストリーミングではなくポーリング前提。[ビルドログ取得](../builds/004-get-build-buildid-logs.md)と同じ方式で、since を省略すると先頭からの全ログ、直前のレスポンスの nextSince を since に渡すと差分のみ返る
- Note: podId は必須。[デプロイ詳細取得](002-get-deploy-deployid.md) の pods 配列から取得した podId を指定し、その Pod 1つのログのみを返す(replicas が複数ある場合、全replica分をまとめて返す機能は無い。複数replicaのログを見たい場合はpodIdを変えて複数回呼び出す)
- Note: ログは収集コンテナが Pod のログを購読して DB に保存する。保持期間は7日で、Pod ごとに上限(50MB)があり、超えたら古い行から削除される
- Note: 保持期間内なら、既に終了・再作成された Pod の podId でもログを取得できる。podId が、その deploy に属したことがない、またはログが保持期間を過ぎて削除済みの場合は 404 を返す。pods 配列は現在稼働している Pod だけを返す(終了した Pod は含まない)
- Note: ログは DB に書き込まれた分を返すので、Pod の出力から少し遅れる(収集コンテナの切り替え中は遅れが大きくなりうる)(仮)
- Note: 削除した deploy のログは、deploy の削除と一緒に削除される
- Note: since に現在のログ末尾を超える値を指定した場合はエラーにせず、logs は空文字列、nextSince は現在のログ末尾の位置を返す

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

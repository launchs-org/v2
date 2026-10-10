[← デプロイ・起動設定関連](README.md)

# デプロイ ログ取得

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Get /deploy/:deployId/logs`

## 説明

deploy を構成する特定の Pod のランタイムログを取得するエンドポイント。[ビルドログ取得](../builds/004-get-build-buildid-logs.md) と同じ `since`/`nextSince` によるポーリング方式。障害発生時の切り分けを目的とする。[デプロイ詳細取得](002-get-deploy-deployid.md) の `pods` 配列から取得した `podId` を指定して呼び出す。

## テスト要件

### 正常系
- 有効な `podId` を指定すると 200 が返り、`logs`/`nextSince` が取得できること
- `since` を省略すると先頭からの全ログが返ること
- 直前のレスポンスの `nextSince` を `since` に指定すると、その時点からの差分ログのみ返ること
- `replicas` が2以上で複数の `podId` が存在する場合、それぞれの `podId` を指定して個別にログを取得できること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `podId` が未指定の場合 400 が返ること
- 指定した `podId` が現在の `pods`(['デプロイ詳細取得'](002-get-deploy-deployid.md) 参照)に存在しない場合(既に終了・再作成されて無くなった Pod を指している場合)404 が返ること
- 他の `deployId` に属する `podId` を指定した場合 404 が返ること

### 境界値
- `since` に現在のログ末尾を超える値を指定した場合、エラーにはせず `logs` は空文字列、`nextSince` は現在のログ末尾の位置を返すこと
- Pod が再起動されて新しい `podId` が割り当てられた場合、古い `podId` を指定するとログが取得できなくなること(k8sのPodログの保持期間・ローテーションに依存するため、恒久的な保管が必要な場合は別途ログ収集基盤の検討が必要。今回は対象外)

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
{"logs": "2026-09-16T00:00:01Z [INFO] server started on :8080\n...", "nextSince": 256}
```
- Note: ストリーミングではなくポーリング前提。[ビルドログ取得](../builds/004-get-build-buildid-logs.md)と同じ方式で、since を省略すると先頭からの全ログ、直前のレスポンスの nextSince を since に渡すと差分のみ返る
- Note: podId は必須。[デプロイ詳細取得](002-get-deploy-deployid.md) の pods 配列から取得した podId を指定し、その Pod 1つのログのみを返す(replicas が複数ある場合、全replica分をまとめて返す機能は無い。複数replicaのログを見たい場合はpodIdを変えて複数回呼び出す)
- Note: 指定した podId が現在の pods に存在しない(既に終了・再作成されて無くなった Pod を指す)場合、404 を返す。Pod が再起動されると新しい podId が割り当てられ、古い podId のログはもう取得できない(k8sのPodログの保持期間・ローテーションに依存するため、恒久的な保管が必要な場合は別途ログ収集基盤の検討が必要)
- Note: since に現在のログ末尾を超える値を指定した場合はエラーにせず、logs は空文字列、nextSince は現在のログ末尾の位置を返す

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)

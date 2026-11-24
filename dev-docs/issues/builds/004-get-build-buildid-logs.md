[← ビルド関連](README.md)

# ビルドログ取得

- カテゴリ: ビルド関連 (/upload, /build)
- Method / Path: `Get /build/:buildId/logs`

## 説明

ビルドのログをポーリング方式(ストリーミングではない)で取得するエンドポイント。`since` に直前レスポンスの `nextSince` を渡すことで差分のみ取得できる。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `since` を省略すると先頭からの全ログが返ること
- 直前のレスポンスの `nextSince` を `since` に指定すると、その時点からの差分ログのみ返ること
- `status` が `"running"` の間、繰り返しポーリングすることで最新ログを追えること
- `status` が `"success"`/`"failed"`/`"canceled"` になった後も、同じ方式でログ全体を取得できること(failedで中断したビルドのログも取得可能)

### 異常系
- 存在しない `buildId` を指定した場合 404 が返ること
- 他ユーザーが所有する `buildId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- ログは30日間保持され、保持期間を過ぎて削除されたログを取得した場合は 404 が返ること(仮)
- プロジェクトを削除すると、そのビルドログも一緒に削除されること
- `since` に現在のログ末尾を超える値を指定した場合、エラーにはせず `logs` は空文字列、`nextSince` は現在のログ末尾の位置を返すこと(クライアントが次回そのまま `since` に使い続けられる)
- ログがまだ1行も出力されていない(`status: "pending"`)状態で呼び出した場合、空文字列が返ること

## 仕様

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
- Note: ログは30日間保持され、その後削除される。プロジェクトを削除すると一緒に削除される
- Note: 保持期間を過ぎて削除されたログを取得した場合は 404 を返す(仮)
- Note: status が "running" の間は繰り返しポーリングすることで最新ログを追える。status が "success" | "failed" | "canceled" になった後も同じ方式でログ全体を取得できる(failed で中断したビルドのログも取得可能)

## 参照

- [dev-docs/endpoints/builds.md](../../endpoints/builds.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

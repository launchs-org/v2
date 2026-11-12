[← エンドポイント一覧](README.md)

# 共通ルール

全グループのエンドポイントに共通するルール。`docs/pre-implementation-checklist.md` の回答(B-1〜B-7 ほか)をもとにまとめた。各グループの仕様書への個別の反映は、グループごとに確認しながら進める。

## パス
- 認証コンテナのエンドポイントは `/auth/...`、それ以外は `/api/v1/...`。Traefik がパスでコンテナへ振り分ける。
- 各仕様書のパスは、このプレフィックスを省いた形で書く。実際の URL は、認証が `/auth` + パス、それ以外が `/api/v1` + パス(例: `POST /projects` の実際の URL は `POST /api/v1/projects`)。

## ID
- 接頭辞 + UUID v4。接頭辞はリソースの種類ごとに付ける(例: `project-` `build-` `deploy-` `service-` `ingress-` `volume-` `userid-`)。
- 例: `project-6f1c2a3e-8b4d-4e5f-9a1b-2c3d4e5f6a7b`

## 日時
- ISO 8601 で、JST(`+09:00`)を基準にする。例: `2026-09-16T09:00:00+09:00`
- 末尾が `Z`(UTC)の形は使わない。
- DB の列は `timestamptz` にして、アプリ側で JST に揃える。

## 名前
- project・volume などの `name` と、`userName`: 英数字とハイフンのみ、最大64文字。
- volume の `name` は project 内で一意。
- project の `name` は同じユーザーの中で一意(仮)。
- `userName` は大文字小文字を区別せずに重複を判定する(仮)。

## エラーレスポンス
エラー時のボディは次の形にする。

```json
{"error": {"code": "not_found", "message": "..."}}
```

ステータスコードと `code`(`code` は仮):

| ステータス | code | 使いどころ |
|---|---|---|
| 400 | `invalid_request` | リクエストの形式や値が不正 |
| 401 | `unauthorized` | 認証なし、トークンが無効・期限切れ、ログイン失敗 |
| 404 | `not_found` | 存在しない、または他ユーザーのリソース |
| 409 | `conflict` | 排他制御(`applying` / `deleting` 中)、状態に合わない操作、名前などの重複 |
| 413 | `payload_too_large` | アップロードが上限を超えた |
| 429 | `too_many_requests` | レート制限(`Retry-After` ヘッダ付き) |
| 500 | `internal_error` | サーバー内部のエラー |

403 は、リソースの存在を隠す必要がない権限エラー(例: 管理者用の操作)に限って使う。現時点で使う場面は無い。

## 認可
- 他ユーザーのリソースにアクセスした場合は 404 を返す(リソースの存在を隠すため)。

## 排他制御と削除
- `applying` 中・`deleting` 中のリソースに対する変更系の操作は、409 を返す。
- 削除は Workflow で非同期に行う。削除を受け付けると、対象と配下のリソースの `status` が `deleting` になる。
- `deleting` 中の GET は `deleting` を返し、削除が完了したら 404 になる。

## ページング
- 一覧系(project、build 履歴、deploy 履歴、ingress、volume)は limit + cursor 方式。
- クエリ: `limit`(デフォルト20、最大100。仮)、`cursor`(前回のレスポンスの `nextCursor`。省略すると先頭から)
- 範囲外の `limit`(1〜100 以外)と、不正な `cursor` は 400 を返す(仮)。
- レスポンス: 従来の配列のキーに加えて、`nextCursor`(最後のページなら `null`)を返す。`cursor` の中身はクライアントが解釈しない文字列として扱う。
- 並び順は作成日時の降順。

```
GET /projects?limit=20&cursor=...
```
```json
{"projects": [{"projectId": "project-...", "name": "my-app"}], "nextCursor": "..."}
```

## 運用のためのエンドポイント
- `GET /healthz`: サーバーが動いているかを返す。認証は不要で、`/api/v1` の外にある。200 と `{"status": "ok"}` を返す。
- k8s の死活監視(liveness / readiness probe)などの、運用のために使う。データベースなどの外部には問い合わせず、サーバーのプロセスがリクエストを受けられるかだけを返す。

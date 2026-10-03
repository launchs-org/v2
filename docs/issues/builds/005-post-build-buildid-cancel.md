[← ビルド関連](README.md)

# ビルドキャンセル

- カテゴリ: ビルド関連 (/upload, /build)
- Method / Path: `Post /build/:buildId/cancel`

## 説明

進行中のビルドをキャンセルするエンドポイント。呼び出すと即座に `status` が `"canceling"` になり、実際の中断処理が完了すると `"canceled"` に遷移する。

## テスト要件

### 正常系
- `status` が `"pending"` または `"running"` のビルドに対して呼び出すと 200 が返り、`status` が即座に `"canceling"` になること
- 中断処理完了後、[ビルド状態取得](003-get-build-buildid.md) で `status` が `"canceled"` になること

### 異常系
- 存在しない `buildId` を指定した場合 404 が返ること
- 他ユーザーが所有する `buildId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 既に `"canceling"` | `"success"` | `"failed"` | `"canceled"` のビルドに対して呼び出した場合 409 が返ること(多重キャンセル不可)

### 境界値
- キャンセル要求を受け付け `"canceling"` に遷移した後、中断処理が完了するより先にビルド自体が自然に成功/失敗した場合は、キャンセル要求はそのまま不成立となり、先に確定した `"success"`/`"failed"` が最終的な `status` になること(キャンセルがビルドの完了を上書きしない)

## 仕様

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
- Note: "canceling" への遷移後、中断が完了するより先にビルドが自然に成功/失敗した場合は、キャンセル自体が不成立になり、先に確定した "success"/"failed" が最終 status になる(キャンセル要求がビルドの完了より後に追いつけなかった形。中断処理はベストエフォートであり、完了済みのビルドを後から失敗扱いにはしない)
- Note: 既に "canceling" | "success" | "failed" | "canceled" の場合は 409 を返す(多重キャンセル不可)

## 参照

- [docs/endpoints/builds.md](../../endpoints/builds.md)

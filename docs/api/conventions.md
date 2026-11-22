# 共通のルール

正本は [docs/endpoints/common.md](../endpoints/common.md)。

## 形式

- JSON(`Content-Type: application/json`)。日時は **JST の ISO 8601**(例: `2026-11-22T20:00:00+09:00`)。
- パスは `/api/v1/...`。

## エラー

```json
{"error": {"code": "not_found", "message": "..."}}
```

| HTTP | code | 意味 |
|---|---|---|
| 400 | `invalid_request` | 入力が不正(項目、形式、範囲) |
| 401 | `unauthorized` | 認証が無い、無効 |
| 403 | `forbidden` | 認証はできたが、権限が足りない(管理者用の操作、API トークンの範囲外) |
| 404 | `not_found` | 無い、または**他のユーザーのもの** |
| 409 | `conflict` | 状態に合わない(反映中、削除中、クォータ超過、重複) |
| 503 | `service_unavailable` | 機能が無効、外部に届かない |
| 500 | `internal_error` | サーバー内部のエラー |

`message` は日本語の文。**分岐には `code` を使う**(`message` は変わりうる)。

## ページ送り

一覧は `limit`(最大 100)と `cursor` を使う。

```bash
curl -sS -H "Authorization: Bearer $TOKEN" "$LAUNCHS/api/v1/projects?limit=20"
# → {"projects":[...], "nextCursor":"..."}      最後のページは "nextCursor": null
curl -sS -H "Authorization: Bearer $TOKEN" "$LAUNCHS/api/v1/projects?limit=20&cursor=..."
```

`cursor` の中身は、解釈しない。

## 「保存 → 反映」の 2 段階

設定の変更(起動設定、マウント、ファイル、ConfigMap のマウント、Service のポート、Ingress のルール、ボリュームの大きさ)は、**すぐには実際に反映されない**。

```
PUT  …/config           反映待ち(pending_*)として保存。まだ何も起きない
POST …/config/apply     反映を始める(すぐ応答する。実際の反映は非同期)
POST …/config/discard   反映待ちを捨てる
```

- `GET /deploy/:deployId` の `config.pending_*`(と `pending_image_ref`)で、反映待ちを確かめられる。
- 送った内容が、いまと同じなら、反映待ちは作られない。
- **反映中(`applying`)・削除中の deploy への変更は 409。** 少し待って、やり直す。

### 反映の終わりを待つ

`apply` は、すぐに応答する。結果は、deploy の `status` で見る。

| status | 意味 |
|---|---|
| `pending` | 作成を受け付け、起動を確認中 |
| `applying` | 反映中(5 分で、終わらなければ `failed`) |
| `active` | 稼働中 |
| `scaled_zero` | 台数が 0 |
| `failed` | 失敗(元の値のまま動いている。反映待ちは残る) |
| `deleting` / `delete_failed` | 削除中 / 削除に失敗 |

```bash
# applying が終わるまで待つ例
until [ "$(curl -sS -H "Authorization: Bearer $TOKEN" $LAUNCHS/api/v1/deploy/$DEPLOY | jq -r .status)" != applying ]; do sleep 3; done
```

**失敗しても、動いていたものは、元の値のまま残る。** 直して再度 `apply` するか、`discard` する。

## 削除

削除も非同期(`deleting` → 消える)。プロジェクトの削除は、中の deploy、Ingress、ボリューム(データごと)も消す。**元に戻せない。**

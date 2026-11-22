# タグとメトリクス

仕様は [tags.md](../endpoints/tags.md)。

## タグ

- deploy、volume、service、ingress に付けられる。**10 個まで**。小文字の英数字と `. _ : -`。
- 付ける: `PUT /deploy/:deployId/tags` `{"tags": ["web","prod"]}`(全件を置き換える)。volume、service、ingress は `PUT …/tags`。
- テンプレートから作ったものには、`template:<id>` と `apply:<適用の識別>` が、自動で付く。
- 一覧: `GET /projects/:projectId/tags`(タグごとの数)。

```bash
# タグ「apply:xxx」のものを、まとめて削除(URL エンコードに注意)
curl -sS -X DELETE "$LAUNCHS/api/v1/projects/$PROJECT/tags/apply%3Axxx/deploys" -H "Authorization: Bearer $TOKEN"
# → {"accepted":[…],"failed":[{"kind":"deploy","id":"…","message":"…"}]}
```

- 削除の順は、**ingress → service → deploy**。**volume と ConfigMap は、対象外**(データを守るため)。
- 反映中・起動中のものは、受け付けられず、`failed` に入る(全体は 200)。**`failed` を、必ず確かめる**。

## メトリクス

| パス | 内容 |
|---|---|
| `GET /deploy/:deployId/metrics` | deploy の CPU・メモリ・Pod 数・再起動 |
| `GET /projects/:projectId/metrics` | プロジェクト全体 |
| `GET /projects/:projectId/tags/:tag/metrics` | タグごと |

- `range=1h|6h|24h`。**30 秒ごとの観測で、24 時間分だけ**保存する。
- CPU とメモリは、クラスターの metrics-server から。無いときは `null`(Pod 数などは出る)。

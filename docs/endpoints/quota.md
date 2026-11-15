[← エンドポイント一覧](README.md)

## クォータ関連 (/quota)

アカウントごとの上限。パス、エラー形式などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/quota`)。設計は [docs/auth.md](../auth.md) の §8。

### 上限を超えたとき
- 作る・増やす操作が、上限を超えるなら、**409 `conflict`**(メッセージに、どの項目が、いくつ中いくつか、を入れる)。
  - 例: `プロジェクトの数が上限(10)に達しています(使用中 10)`、`CPU の合計が上限(4000ミリコア)を超えます(使用中 3750、追加 500)`。
- 確かめる操作: プロジェクトの作成、deploy の作成、スケールの増加(replicas を増やすとき)、volume の作成、volume の拡張の保存(`PUT .../volumes/:volumeId` の大きさ)。
  - 減らす・消す・`scaled_zero` に戻す操作は、確かめない。
  - volume の拡張は、保存の時点(pending)で、予約として数える。apply のときは、確かめない。
- volume 1つの大きさの上限(`maxVolumeSizeMB`)は、クォータとは別で、超えたら、これまでどおり **400**。
- 既存のデータが、あとから下げた上限を超えていても、消さない(新しく作れなくなるだけ)。

### 項目と数え方

| 項目 | 数え方 |
|---|---|
| `projects` | `deleting` 以外のプロジェクトの数 |
| `deploys` | `deleting` 以外の deploy の数(service は deploy に1つなので、数えない。ingress は、上限なし) |
| `cpuMillicores` | `deleting` 以外の deploy の「replicas × スペックの CPU」の合計(`scaled_zero` は 0) |
| `memoryMiB` | 同じく、メモリ(MiB)の合計 |
| `volumes` | volume の数 |
| `volumeTotalMB` | 全 volume の、現在の大きさと pending の大きさの大きい方(MB)の合計 |

上限の初期値は、プロジェクト 10、deploy 20、CPU 4000 ミリコア、メモリ 8192 MiB、volume 10、volume の合計 20480 MB。DB の `quota_defaults`(全員の既定)と `account_quotas`(ユーザーごとの上書き。NULL の列は既定)で決まり、更新すれば変わる。変更の API は、アカウント管理の PR で作る。

### 自分の上限と使用量の取得
- Get: /quota
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "projects":      {"limit": 10,    "used": 2},
  "deploys":       {"limit": 20,    "used": 5},
  "cpuMillicores": {"limit": 4000,  "used": 1250},
  "memoryMiB":     {"limit": 8192,  "used": 1280},
  "volumes":       {"limit": 10,    "used": 1},
  "volumeTotalMB": {"limit": 20480, "used": 1024},
  "maxVolumeSizeMB": 5120
}
```
- Note
  - ロックはしない(見るだけ)。使用量は、取得した時点の値で、このあとの操作で変わる。

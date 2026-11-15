[← エンドポイント一覧](README.md)

## クォータ関連 (/quota)

アカウントごとの上限。パス、エラー形式などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/quota`)。設計は [docs/auth.md](../auth.md) の §8。

### 上限を超えたとき
- 作る・増やす操作が、上限を超えるなら、**409 `conflict`**(メッセージに、どの項目が、いくつ中いくつか、を入れる)。
  - 例: `プロジェクトの数が上限(10)に達しています(使用中 10)`、`large のインスタンスの合計が上限(5個)を超えます(使用中 4、追加 2)`。
- 確かめる操作: プロジェクトの作成、deploy の作成(数と、スペックごとのインスタンス数)、スケールの増加(replicas を増やすとき)、volume の作成、volume の拡張の保存(`PUT .../volumes/:volumeId` の大きさ)。
  - 減らす・消す・`scaled_zero` に戻す操作は、確かめない。
  - volume の拡張は、保存の時点(pending)で、予約として数える。apply のときは、確かめない。
- volume 1つの大きさの上限(`maxVolumeSizeMB`)は、クォータとは別で、超えたら、これまでどおり **400**。
- 既存のデータが、あとから下げた上限を超えていても、消さない(新しく作れなくなるだけ)。

### 項目と数え方

| 項目 | 数え方 |
|---|---|
| `projects` | `deleting` 以外のプロジェクトの数 |
| `deploys` | `deleting` 以外の deploy の数(service は deploy に1つなので、数えない。ingress は、上限なし) |
| `instances` | スペック(`small`、`medium`、`large` など)の名前ごとの、`deleting` 以外の deploy の `replicas` の合計(`scaled_zero` は 0)。「large は 5 個まで」のように、スペックごとに上限がある |
| `volumes` | volume の数 |
| `volumeTotalMB` | 全 volume の、現在の大きさと pending の大きさの大きい方(MB)の合計 |

上限の初期値は、プロジェクト 10、deploy 20、volume 10、volume の合計 20480 MB、インスタンスは small 20 個・medium 10 個・large 5 個。DB で決まり、更新すれば変わる。変更の API は、アカウント管理の PR で作る。
- `quota_defaults`(全員の既定)と `account_quotas`(ユーザーごとの上書き。NULL の列は既定)。
- スペックごとのインスタンス数の既定は `deploy_specs.default_max_instances`(スペックを足すときに、上限も決める)。ユーザーごとの上書きは `account_spec_quotas`(行が無いスペックは既定)。

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
  "volumes":       {"limit": 10,    "used": 1},
  "volumeTotalMB": {"limit": 20480, "used": 1024},
  "instances": {
    "small":  {"limit": 20, "used": 3},
    "medium": {"limit": 10, "used": 0},
    "large":  {"limit": 5,  "used": 2}
  },
  "maxVolumeSizeMB": 5120
}
```
- Note
  - ロックはしない(見るだけ)。使用量は、取得した時点の値で、このあとの操作で変わる。

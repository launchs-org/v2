[← エンドポイント一覧](README.md)

## クォータ関連 (/quota)

アカウントごとの上限。パス、エラー形式などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/quota`)。設計は [docs/auth.md](../auth.md) の §8。

### 上限を超えたとき
- 作る・増やす操作が、上限を超えるなら、**409 `conflict`**(メッセージに、どの項目が、いくつ中いくつか、を入れる)。
  - 例: `プロジェクトの数が上限(10)に達しています(使用中 10)`、`large の deploy の数が上限(5)に達しています(使用中 5)`。
- 確かめる操作: プロジェクトの作成、deploy の作成(合計の数と、スペックごとの数)、volume の作成、volume の拡張の保存(`PUT .../volumes/:volumeId` の大きさ)。
  - 消す操作は、確かめない。
- **スケール(replicas)は、クォータの対象ではない。** 1つの deploy の replicas は、全体で 0〜5(`maxReplicas`)。スペックにも、アカウントにも関係なく、超えたら **400**(入力の値の問題)。
  - volume の拡張は、保存の時点(pending)で、予約として数える。apply のときは、確かめない。
- volume 1つの大きさの上限(`maxVolumeSizeMB`)は、クォータとは別で、超えたら、これまでどおり **400**。
- 既存のデータが、あとから下げた上限を超えていても、消さない(新しく作れなくなるだけ)。

### 項目と数え方

| 項目 | 数え方 |
|---|---|
| `projects` | `deleting` 以外のプロジェクトの数 |
| `deploys` | `deleting` 以外の deploy の数(service は deploy に1つなので、数えない。ingress は、上限なし) |
| `deploysBySpec` | スペック(`small`、`medium`、`large` など)の名前ごとの、`deleting` 以外の deploy の数。「large は 5 個まで」のように、スペックごとに上限がある。**replicas(スケール)は数えない** |
| `volumes` | volume の数 |
| `volumeTotalMB` | 全 volume の、現在の大きさと pending の大きさの大きい方(MB)の合計 |

上限の初期値は、プロジェクト 10、deploy 20、volume 10、volume の合計 20480 MB、スペックごとの deploy は small 20 個・medium 10 個・large 5 個。DB で決まり、更新すれば変わる。変更は、管理者の API(下)で行う。
- `quota_defaults`(全員の既定)と `account_quotas`(ユーザーごとの上書き。NULL の列は既定)。
- スペックごとの deploy の数の既定は `deploy_specs.default_max_deploys`(スペックを足すときに、上限も決める)。ユーザーごとの上書きは `account_spec_quotas`(行が無いスペックは既定)。

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
  "deploysBySpec": {
    "small":  {"limit": 20, "used": 3},
    "medium": {"limit": 10, "used": 0},
    "large":  {"limit": 5,  "used": 2}
  },
  "maxVolumeSizeMB": 5120,
  "maxReplicas": 5
}
```
- Note
  - ロックはしない(見るだけ)。使用量は、取得した時点の値で、このあとの操作で変わる。

## 管理者の API (/admin/users/:userId/quota)

ユーザーごとの上限(上書き)の確認と設定。**管理者だけ**が使える。
- Header: `{"Authorization": "Bearer <accessToken>"}`(access token の `role` が `admin`)。
- トークンが無い・使えない: 401。管理者でない: **403** `forbidden`。
- **管理者かどうかは、access token の `role` で見る**(backend は、ユーザーの DB を持たない)。管理者でなくなった人が、access token の残り(最大15分)、通ることがある。auth の管理 API(`/auth/admin`)は、毎回 DB で確かめるので、この遅れは無い。開発用の仮の認証(`DEV_USER_ID`)では、role が無いので、常に 403。
- `userId` は、auth のユーザー ID。backend は、ユーザーが実在するかを知らない(形だけを確かめる。英数字とハイフン、100文字まで。不正は 400)。存在しないユーザーの ID を指定しても、上書きが保存されるだけで、害は無い。
- 設定は、auth のログではなく、backend のログに残る(誰が、誰の上限を、どう変えたか)。

### 上限と使用量・上書きの取得
- Get: /admin/users/:userId/quota
- Response (200): `GET /quota` と同じ内容(そのユーザーの上限と使用量)に、`overrides`(管理者が設定した上書き。無い項目は `null`)が付く。
```json
{
  "projects": {"limit": 20, "used": 2},
  "deploys": {"limit": 20, "used": 5},
  "deploysBySpec": {"small": {"limit": 20, "used": 3}, "medium": {"limit": 10, "used": 0}, "large": {"limit": 2, "used": 2}},
  "volumes": {"limit": 10, "used": 1},
  "volumeTotalMB": {"limit": 20480, "used": 1024},
  "maxVolumeSizeMB": 5120,
  "maxReplicas": 5,
  "overrides": {"projects": 20, "deploys": null, "volumes": null, "volumeTotalMB": null, "deploysBySpec": {"large": 2}}
}
```

### 上書きの設定
- Put: /admin/users/:userId/quota
- Body
```json
{"projects": 20, "deploys": null, "volumes": null, "volumeTotalMB": null, "deploysBySpec": {"large": 2}}
```
- Response (200): 取得と同じ(置き換えた後の状態)。
- Note: **一式の置き換え**。ボディに無い項目、`null` の項目は、既定に戻る(以前の上書きは消える)。全て既定に戻すには `{}`。
- Note: 値は 0〜1000000 の整数(0 は、そのリソースを作れなくする)。`deploysBySpec` のキーは、存在するスペックの名前(無い名前は 400)。**知らない項目名は 400**(書き間違いで、上書きが消えてしまうのを防ぐ)。
- Note: 既にある使用量が、新しい上限を超えても、消さない。新しく作れなくなるだけ。
- Note: 上限の既定(`quota_defaults`、`deploy_specs.default_max_deploys`)を変えるのは、マイグレーションまたは SQL。この API では変えない。

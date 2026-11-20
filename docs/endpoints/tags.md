# タグとメトリクス

deploy・volume・service・ingress に**タグ**を付けて、タグ単位で一斉に削除できる。CPU・メモリなどの**メトリクス**を、deploy、プロジェクト、タグの単位で見られる。共通のルールは [common.md](common.md)。

## タグ

- タグは、**deploy、volume、service、ingress** に付けられる。1 つの資源に 10 個まで。1 つは 40 文字まで。小文字の英数字で始まり、英数字と `.` `_` `:` `-` を使える。重複は 1 つにまとめ、昇順に保存する。
- テンプレートから作った deploy・volume・service・ingress には、**自動で 2 つ付く**: `template:<テンプレートの id>`(同じテンプレートの全て)と、`apply:<適用の識別>`(その 1 回の適用の分だけ)。同じテンプレートを 2 回適用しても、片方だけを `apply:` で消せる。既存の資源には、さかのぼっては付かない。
- 各資源の取得・一覧のレスポンスに `tags`(配列)が入る。ConfigMap は、タグの対象ではない(一斉削除でも残る)。
- 画面: 構成図のカード(タグ 2 個まで)、上のタグ一覧(押すと同じタグを強調。強調中は「このタグを削除」)、「タグ」タブ(一覧と一斉削除)、各パネルの編集欄、「メトリクス」タブ。

### タグを置き換える(PUT)

タグを、渡した組に置き換える(空の配列は、全て外す)。Body: `{"tags": ["web", "prod"]}`(必須)。Response `200`: `{"tags": ["prod", "web"]}`(deploy は `deployId` も)。Errors: `invalid_request`、`not_found`、`unauthorized`

| 資源 | パス |
|---|---|
| deploy | `PUT /deploy/:deployId/tags` |
| service | `PUT /deploy/:deployId/service/tags`(service は deploy と 1 対 1 なので、deploy の ID で指す) |
| volume | `PUT /projects/:projectId/volumes/:volumeId/tags` |
| ingress | `PUT /projects/:projectId/ingresses/:ingressId/tags` |

### GET /projects/:projectId/tags

プロジェクトのタグと、種類ごとの数。

- Response `200`: `{"tags": [{"tag": "template:redash", "deployCount": 4, "volumeCount": 2, "serviceCount": 4, "ingressCount": 1}]}`

### DELETE /projects/:projectId/tags/:tag/deploys

タグが付いた資源の削除を、**一斉に受け付ける**(完了は非同期。各資源の削除と同じ規則で、反映中などは受け付けられない)。順番は **ingress → service → deploy → volume**。1 つが失敗しても、他は続ける。`:tag` の `:` は、`%3A` に符号化されていてよい。

- Query: `keepVolumes=true` で、volume は削除せずに残す(データを残したいとき)。
- Response `200`: `{"accepted": [{"kind": "deploy", "id": "deploy-...", "message": ""}], "failed": [{"kind": "volume", "id": "volume-...", "message": "..."}]}`(`kind` は deploy / volume / service / ingress)
- deploy が消えるまで、その volume は「使用中」で受け付けられないことがある。失敗した分は、少し待って、同じ呼び出しでやり直す。
- Errors: `invalid_request`(タグが不正)、`not_found`(そのタグの資源が無い)、`unauthorized`

## メトリクス

watcher が、30 秒ごとに、全ての deploy の Pod を観測して、DB(`deploy_metric_samples`)に保存する。**24 時間分**を残す。CPU とメモリは metrics-server(`metrics.k8s.io`)から。クラスターに無いときは、`cpuMillicores` と `memoryBytes` が `null` で、Pod の数などだけが入る。

| エンドポイント | 範囲 |
|---|---|
| `GET /deploy/:deployId/metrics?range=` | deploy 1 つ |
| `GET /projects/:projectId/metrics?range=` | プロジェクトの全ての deploy の合計 |
| `GET /projects/:projectId/tags/:tag/metrics?range=` | プロジェクトの中で、タグが付いた deploy の合計 |

- Query: `range` は `1h`(既定)、`6h`、`24h`。
- Response `200`: `{"range": "1h", "points": [{"at": "2026-11-20T01:00:00Z", "cpuMillicores": 120, "memoryBytes": 104857600, "pods": 2, "readyPods": 2, "restarts": 0}]}`(時刻の昇順。`at` は UTC)
- `cpuMillicores`: CPU の使用量の合計(ミリコア。1000 で 1 コア)。`memoryBytes`: メモリの使用量の合計。`pods` / `readyPods`: Pod の数と、Ready の数。`restarts`: コンテナの再起動の回数の合計(いま存在する Pod の分)。
- 削除した deploy の観測値は、deploy と一緒に消える。
- Errors: `invalid_request`(`range` が不正)、`not_found`(deploy が無い)、`unauthorized`

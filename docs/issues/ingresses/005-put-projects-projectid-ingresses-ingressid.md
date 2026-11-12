[← イングレス関連](README.md)

# イングレス更新 (rules 一括置き換え、pendingとして保存)

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Put /projects/:projectId/ingresses/:ingressId`

## 説明

`rules`(path rule の配列)を一括で置き換え、`pending_rules` として保存するエンドポイント。[適用](006-post-projects-projectid-ingresses-ingressid-apply.md) を呼ぶまで実際のルーティングには反映されない。path rule の追加・削除・変更は全てこのエンドポイントで配列ごと送る。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 同一 project 内の `serviceId` と、そのserviceの `ports` に含まれる `"tcp"` ポートを指定した `rules` を送ると 200 が返り、[イングレス取得](003-get-projects-projectid-ingresses-ingressid.md) の `pending_rules` に反映されること
- 呼び出し直後、`rules`(現在反映されている値)は変化しないこと
- 複数の path(例: `/`, `/api`)に異なる `serviceId`/`port` を割り当てられること

### 異常系
- 存在しない `ingressId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`ingressId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 同一 project に属さない(他プロジェクトの)`serviceId` を指定した場合エラーになること
- 対象 `serviceId` の `ports` に存在しない `port` を指定した場合エラーになること
- 対象 `serviceId` の `ports` のうち `protocol` が `"udp"` のポートを指定した場合エラーになること(HTTPのみ対応のため)
- `status` が `"applying"` の間はこの操作を受け付けず 409 が返ること(applying中はrulesに対する変更・apply・discardを排他制御する)
- `status` が `"deleting"` の間はこの操作を受け付けず 409 が返ること

### 境界値
- `rules` は個別rule単位のCRUDではなく、常に全件を送って置き換える方式であること(部分更新は不可)
- 同一 `rules` 配列内で `path` が重複した場合 400 が返ること
- `rules` に空配列を送った場合、全ルールを削除する意図として扱うこと

## 仕様

- Put: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080},
    {"path": "/api", "serviceId": "service-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "port": 8081}
  ]
}
```
- Response (200)
```json
{}
```
- Note: rules は個別rule単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)。path ruleの追加・削除・変更は全てこのエンドポイントで配列ごと送る
- Note: この時点ではまだ反映されない。pending_rules として保存され、[イングレス適用](006-post-projects-projectid-ingresses-ingressid-apply.md) を呼ぶまで rules(現在反映されている値)は変わらない
- Note: serviceId には同一 project 内の任意の deploy に属する service を指定できる。path ごとに向き先の service を切り替えられる
- Note: port には対象serviceの ports 配列に含まれるポート番号を指定する。ingressはHTTPのみ対応のため、指定するportは対象serviceのports配列で protocol が "tcp" のものに限る("udp"のポートを指定した場合はエラーになる)
- Note: ingress の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はrulesに対する変更・apply・discardを排他制御する)。"deleting" の間も 409 を返す

## 参照

- [docs/endpoints/ingresses.md](../../endpoints/ingresses.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

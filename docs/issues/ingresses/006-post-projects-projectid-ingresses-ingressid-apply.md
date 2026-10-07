[← イングレス関連](README.md)

# イングレス 適用

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Post /projects/:projectId/ingresses/:ingressId/apply`

## 説明

[イングレス更新](005-put-projects-projectid-ingresses-ingressid.md) で保存した `pending_rules` を `rules` に反映し、ルーティング設定の書き換えを開始するエンドポイント。

処理順序は「`pending_rules` の値を使って先にルーティング設定への反映を試みる → その結果(成功/失敗)が確定してから、`rules` を `pending_rules` の内容で上書きし `pending_rules` を null に戻す」という順(他グループのapplyと同じ方針)。ingress の反映は deploy のコンテナ再起動を伴わない分、config/service/volumeのapplyより短時間で完了する想定。

## テスト要件

### 正常系
- `pending_rules` が存在する状態で呼び出すと 200 が返り、[イングレス取得](003-get-projects-projectid-ingresses-ingressid.md) の `status` が即座に `"applying"` になること
- `status` が `"applying"` の間、`pending_rules` は消費されずそのまま保持されること
- 反映成功時、`rules` が `pending_rules` の内容に更新され、`pending_rules` が `null` に戻り、`status` が `"active"` になること
- 反映失敗時も、`rules` が `pending_rules` の内容(失敗した新しい値)に更新され、`pending_rules` が `null` に戻り、`status` が `"failed"` になること

### 異常系
- 存在しない `ingressId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`ingressId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_rules` が `null` の場合 409 が返ること
- 既に `status` が `"applying"` の場合は 409 が返ること(多重apply不可)
- `pending_rules` の `serviceId`/`port` が指す参照先が、[イングレス更新](005-put-projects-projectid-ingresses-ingressid.md) 時点から変化していて、apply時点で不正になっている場合(対象serviceが削除済み、該当portが削除済み、該当portのprotocolが`"udp"`に変更された等)は反映を試みずに `status` が `"failed"` になること(更新時点のバリデーションだけでなく、apply実行時にも同じ検証を行う)

### 境界値
- `applying` が 60秒を超えても完了しない場合、強制的に `status` が `"failed"` に遷移すること(config/service/volumeの300秒より短い)

## 仕様

- Post: /projects/:projectId/ingresses/:ingressId/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_rules の内容を rules に反映するため、ルーティング設定の書き換えを開始する。呼び出し自体は即座に応答するが、この時点ではまだ rules は更新されていない。pending_rules が null の場合は 409 を返す
- Note: 呼び出し直後、ingress の status は "applying" になる。この間 pending_rules はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_rules の値を使って先にルーティング設定への反映を試み、その結果(成功・失敗)が確定した時点で初めて rules を pending_rules の内容に更新し、pending_rules は null に戻す。成功すれば status は "active" に、失敗すれば "failed" になる(rules の内容自体は失敗時も新しい値で上書きされる)
- Note: ingressの反映はdeployのコンテナ再起動を伴わないルーティング設定の書き換えのみのため、config/volumes/serviceのapplyより短時間で完了する想定だが、確認手順(status のポーリング)は同様
- Note: 既に ingress の status が "applying" の場合は 409 を返す(多重apply不可)
- Note: applying には 60秒 のタイムアウトを設ける。この時間内に反映完了が確認できない場合、強制的に status を "failed" に遷移させる
- Note: pending_rules に含まれる serviceId/port の妥当性([イングレス更新](005-put-projects-projectid-ingresses-ingressid.md)時点のバリデーションと同じ内容: 同一project内のserviceであること、対象serviceのportsに存在するport番号であること、protocolが"tcp"であること)は、apply実行時にも改めて検証する。[イングレス更新](005-put-projects-projectid-ingresses-ingressid.md)から[適用](006-post-projects-projectid-ingresses-ingressid-apply.md)までの間にservice側のポート構成が変更される可能性があるため

## 参照

- [docs/endpoints/ingresses.md](../../endpoints/ingresses.md)

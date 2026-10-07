[← サービス関連](README.md)

# サービス更新 (一括置き換え、pendingとして保存)

- カテゴリ: サービス関連 (/deploy/:deployId/service)
- Method / Path: `Patch /deploy/:deployId/service`

## 説明

`ports` を一括で置き換え、`pending_ports` として保存するエンドポイント。[適用](004-post-deploy-deployid-service-apply.md) を呼ぶまで実際には反映されない。ポートの追加・削除・変更は全てこのエンドポイントで配列ごと送る。

## テスト要件

### 正常系
- `ports` を指定すると 200 が返り、[サービス取得](002-get-deploy-deployid-service.md) の `pending_ports` に反映されること
- 呼び出し直後、`ports`(現在反映されている値)は変化しないこと

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合アクセスできないこと(403 または 404)
- service が未作成の `deployId` に対して呼び出した場合 404 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `ports` が未指定、または空配列の場合の扱い(全ポート削除を意味するか、400にするか)は仕様未確定のため別途定義する
- `status` が `"applying"` の間はこの操作を受け付けず 409 が返ること(applying中はserviceに対する変更・apply・discardを排他制御する)

### 境界値
- `ports` は個別要素単位のCRUDではなく、常に全件を送って置き換える方式であること(部分更新は不可)
- 既存の `pending_ports` が存在する状態で再度PATCHした場合、新しい内容で `pending_ports` が上書きされること(前回のpendingは破棄される)

## 仕様

- Patch: /deploy/:deployId/service
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "ports": [
    {"name": "web", "port": 8080, "protocol": "tcp"},
    {"name": "game", "port": 27015, "protocol": "udp"}
  ]
}
```
- Response (200)
```json
{}
```
- Note: ports は個別要素単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)。ポートの追加・削除・変更は全てこのエンドポイントで配列ごと送る
- Note: この時点ではまだ反映されない。pending_ports として保存され、サービスの apply を呼ぶまで ports(現在反映されている値)は変わらない
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はserviceに対する変更・apply・discardを排他制御する)

## 参照

- [docs/endpoints/services.md](../../endpoints/services.md)

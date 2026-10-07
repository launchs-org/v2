[← サービス関連](README.md)

# サービス 適用

- カテゴリ: サービス関連 (/deploy/:deployId/service)
- Method / Path: `Post /deploy/:deployId/service/apply`

## 説明

[サービス更新](003-patch-deploy-deployid-service.md) で保存した `pending_ports` を `ports` に反映し、コンテナの再起動を開始するエンドポイント。

処理順序は「`pending_ports` の値を使って先に Kubernetes 側へ実際の反映(再起動)を試みる → その結果(成功/失敗)が確定してから、`ports` を `pending_ports` の内容で上書きし `pending_ports` を null に戻す」という順([デプロイ 起動設定 適用](../deploys/007-post-deploy-deployid-config-apply.md) と同じ方針)。

## テスト要件

### 正常系
- `pending_ports` が存在する状態で呼び出すと 200 が返り、[デプロイ詳細取得](../deploys/002-get-deploy-deployid.md) の `status` が即座に `"applying"` になること
- `status` が `"applying"` の間、`pending_ports` は消費されずそのまま保持されること
- 再起動成功時、`ports` が `pending_ports` の内容に更新され、`pending_ports` が `null` に戻り、deploy の `status` が `"active"` になること
- 再起動失敗時も、`ports` が `pending_ports` の内容(失敗した新しい値)に更新され、`pending_ports` が `null` に戻り、deploy の `status` が `"failed"` になること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合アクセスできないこと(403 または 404)
- service が未作成の `deployId` に対して呼び出した場合 404 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_ports` が `null` の場合 409 が返ること
- 既に deploy の `status` が `"applying"` の場合(別のconfig/volumes/service applyが進行中、またはこのapply自体が既に実行中)は 409 が返ること(多重apply不可)

### 境界値
- `applying` が 300秒(5分)を超えても完了しない場合、強制的に deploy の `status` が `"failed"` に遷移すること

## 仕様

- Post: /deploy/:deployId/service/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_ports の内容を ports に反映するため、コンテナの再起動を開始する。呼び出し自体は即座に応答するが、この時点ではまだ ports は更新されていない。pending_ports が null の場合は 409 を返す
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 service の pending_ports はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_ports の値を使って先に Kubernetes 側への反映(コンテナ再起動)を試み、その結果(成功・失敗)が確定した時点で初めて service の ports を pending_ports の内容に更新し、pending_ports は null に戻す。成功すれば deploy の status は "active" に、失敗すれば "failed" になる(service の ports の内容自体は失敗時も新しい値で上書きされる)([デプロイ詳細取得](../deploys/002-get-deploy-deployid.md) でポーリングして確認する)
- Note: 既に deploy の status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に deploy の status を "failed" に遷移させる

## 参照

- [docs/endpoints/services.md](../../endpoints/services.md)

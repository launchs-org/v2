[← ボリューム関連](README.md)

# デプロイ ボリュームマウント 適用

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Post /deploy/:deployId/mounts/apply`

## 説明

[マウント設定](010-put-deploy-deployid-mounts.md) で保存した `pending_mounts` を `mounts` に反映し、コンテナの再起動を開始するエンドポイント。deploy の replicas が 0(status が "scaled_zero")のときは、再起動する Pod が無いのですぐに完了し、成功しても status は "active" ではなく "scaled_zero" に戻る。失敗は "failed" になる。

処理順序は「`pending_mounts` の値を使って先に Kubernetes 側へ実際の反映(再起動)を試みる → その結果(成功/失敗)が確定してから、`mounts` を `pending_mounts` の内容で上書きし `pending_mounts` を null に戻す」という順(他グループのapplyと同じ方針)。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_mounts` が存在する状態で呼び出すと 200 が返り、[デプロイ詳細取得](../deploys/002-get-deploy-deployid.md) の `status` が即座に `"applying"` になること
- `status` が `"applying"` の間、`pending_mounts` は消費されずそのまま保持されること
- 再起動成功時、`mounts` が `pending_mounts` の内容に更新され、`pending_mounts` が `null` に戻り、deploy の `status` が `"active"` になること
- deploy の `replicas` が 0(`status` が `"scaled_zero"`)の場合、再起動する Pod が無いのですぐに完了し、成功しても `status` は `"active"` ではなく `"scaled_zero"` に戻ること。失敗は `"failed"` になること
- 再起動失敗時も、`mounts` が `pending_mounts` の内容(失敗した新しい値)に更新され、`pending_mounts` が `null` に戻り、deploy の `status` が `"failed"` になること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_mounts` が無い場合 409 が返ること
- deploy の `status` が `"deleting"` の場合は 409 が返ること
- 既に deploy の `status` が `"applying"` の場合(別のconfig/service/volumeのapplyが進行中、またはこのapply自体が既に実行中)は 409 が返ること(多重apply不可)
- `pending_mounts` に含まれる `volumeId` が、[マウント設定](010-put-deploy-deployid-mounts.md) 時点から変化していて apply時点で不正になっている場合(対象volumeが削除済み、他プロジェクトに移動している等)は反映を試みずに deploy の `status` が `"failed"` になること(設定時点のバリデーションだけでなく、apply実行時にも同じ検証を行う)

### 境界値
- `applying` が 300秒(5分)を超えても完了しない場合、強制的に deploy の `status` が `"failed"` に遷移すること

## 仕様

- Post: /deploy/:deployId/mounts/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_mounts の内容を mounts に反映するため、コンテナの再起動を開始する。呼び出し自体は即座に応答するが、この時点ではまだ mounts は更新されていない。pending_mounts が無い場合は 409 を返す
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_mounts はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_mounts の値を使って先に Kubernetes 側への反映(コンテナ再起動)を試み、その結果(成功・失敗)が確定した時点で初めて mounts を pending_mounts の内容に更新し、pending_mounts は null に戻す。成功すれば status は "active" に、失敗すれば status は "failed" になる(mounts の内容自体は失敗時も新しい値で上書きされる。実際に起動できているかは status で判断する)([デプロイ詳細取得](../deploys/002-get-deploy-deployid.md) でポーリングして確認する)
- Note: 既に status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に status を "failed" に遷移させる
- Note: deploy の replicas が 0(status が "scaled_zero")のときは、再起動する Pod が無いのですぐに完了し、成功しても status は "active" ではなく "scaled_zero" に戻る。失敗は "failed" になる
- Note: deploy の status が "deleting" の間は 409 を返す
- Note: pending_mounts に含まれる volumeId の妥当性(同一project内のvolumeであること)は、apply実行時にも改めて検証する。[マウント設定](010-put-deploy-deployid-mounts.md)から[適用](011-post-deploy-deployid-mounts-apply.md)までの間にvolume側の状態が変更される可能性があるため

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

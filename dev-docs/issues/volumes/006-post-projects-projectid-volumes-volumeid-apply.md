[← ボリューム関連](README.md)

# ボリューム 適用

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Post /projects/:projectId/volumes/:volumeId/apply`

## 説明

[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md) で保存した `pending_sizeMB` を `sizeMB` に反映する処理(ストレージのリサイズ)を開始するエンドポイント。volume をマウントしている deploy のコンテナ再起動は伴わない(ストレージ側の変更のみ)。

処理順序は「`pending_sizeMB` の値を使って先にストレージ側への実際の反映(リサイズ)を試みる → その結果(成功/失敗)が確定してから、`sizeMB` を `pending_sizeMB` の内容で上書きし `pending_sizeMB` を null に戻す」という順(他グループのapplyと同じ方針)。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_sizeMB` が存在する状態で呼び出すと 200 が返り、[ボリューム取得](003-get-projects-projectid-volumes-volumeid.md) の `status` が即座に `"applying"` になること
- `status` が `"applying"` の間、`pending_sizeMB` は消費されずそのまま保持されること
- 反映成功時、`sizeMB` が `pending_sizeMB` の内容に更新され、`pending_sizeMB` が `null` に戻り、`status` が `"active"` になること
- 反映失敗時も、`sizeMB` が `pending_sizeMB` の内容(失敗した新しい値)に更新され、`pending_sizeMB` が `null` に戻り、`status` が `"failed"` になること
- このapply実行中、volume をマウントしている deploy のコンテナが再起動されないこと(ストレージ側の変更のみで完結する)

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_sizeMB` が `null` の場合 409 が返ること
- 既に `status` が `"applying"` の場合は 409 が返ること(多重apply不可)
- `status` が `"deleting"` の場合は 409 が返ること

### 境界値
- `applying` が 300秒(5分)を超えても完了しない場合、強制的に `status` が `"failed"` に遷移すること

## 仕様

- Post: /projects/:projectId/volumes/:volumeId/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_sizeMB の内容を sizeMB に反映する処理(ストレージのリサイズ)を開始する。呼び出し自体は即座に応答するが、この時点ではまだ sizeMB は更新されていない。pending_sizeMB が null の場合は 409 を返す
- Note: 呼び出し直後、volume の status は "applying" になる。この間 pending_sizeMB はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_sizeMB の値を使って先にストレージ側への反映を試み、その結果(成功・失敗)が確定した時点で初めて sizeMB を pending_sizeMB の内容に更新し、pending_sizeMB は null に戻す。成功すれば status は "active" に、失敗すれば "failed" になる
- Note: このapplyは volume をマウントしている deploy のコンテナ再起動を伴わない(ストレージ側の変更のみ)
- Note: 既に volume の status が "applying" の場合は 409 を返す(多重apply不可)。"deleting" の間も 409 を返す
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に反映完了が確認できない場合、強制的に status を "failed" に遷移させる

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

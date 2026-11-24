[← デプロイ・起動設定関連](README.md)

# デプロイ 起動設定 適用

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Post /deploy/:deployId/config/apply`

## 説明

[起動設定更新](006-put-deploy-deployid-config.md) で保存した `pending_env`/`pending_command`/`pending_args` を `env`/`command`/`args` に反映し、コンテナの再起動を開始するエンドポイント。

処理順序は「`pending_*` の値を使って先に Kubernetes 側へ実際の反映(再起動)を試みる → その結果が確定してから、成功なら `env`/`command`/`args` を `pending_*` の内容に昇格し `pending_*` を null に戻す」という順。つまり `env`/`command`/`args` は常に「いま動いている値」を表し、失敗したときは Kubernetes を元の値に戻した上で、`pending_*`(試した値)を残して `status` を `"failed"` にする。 エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_*` が存在する状態で呼び出すと 200 が返り、[デプロイ詳細取得](002-get-deploy-deployid.md) の `status` が即座に `"applying"` になること
- `status` が `"applying"` の間、`pending_env`/`pending_command`/`pending_args` は消費されずそのまま保持されること
- 再起動成功時、`env`/`command`/`args` が `pending_*` の内容に更新され、`pending_env`/`pending_command`/`pending_args` が `null` に戻り、`status` が `"active"` になること
- `replicas` が 0(`status` が `"scaled_zero"`)の deploy に apply した場合、再起動する Pod が無いのですぐに完了し、成功しても `status` は `"active"` ではなく `"scaled_zero"` に戻ること(失敗は `"failed"`)
- 再起動失敗時は、`env`/`command`/`args` が変わらず(いま動いている値のまま)、`pending_*`(失敗した新しい値)が残り、`status` が `"failed"` になること。Kubernetes の Deployment は元の値に戻されること
- 失敗(`failed`)のあと、`pending_*` を直して PUT し直す、またはそのまま再度 apply できること(discard もできること)

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_env`/`pending_command`/`pending_args` が全て `null`(何も更新していない)の場合 409 が返ること
- 既に `status` が `"applying"` の場合(別のconfig/volumes/service applyが進行中、またはこのapply自体が既に実行中)は 409 が返ること(多重apply不可)
- `status` が `"deleting"` の間に呼び出した場合 409 が返ること

### 境界値
- `applying` が 300秒(5分)を超えても完了しない場合、強制的に `status` が `"failed"` に遷移すること

## 仕様

- Post: /deploy/:deployId/config/apply
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_env/pending_command/pending_args の内容を env/command/args に反映するため、コンテナの再起動を開始する。呼び出し自体は即座に応答するが、この時点ではまだ env/command/args は更新されていない。pending_env/pending_command/pending_args が全て null(何も更新していない)の場合は 409 を返す
- Note: 呼び出し直後、deploy の status は "applying" になる。この間 pending_env/pending_command/pending_args はまだ消費されず、適用しようとしている内容を保持したままとなる。バックエンドは pending_* の値を使って先に Kubernetes 側への反映(コンテナ再起動)を試み、その結果が確定した時点で、成功なら env/command/args を pending_* の内容に更新して pending_* を null に戻す。成功すれば status は "active" に、失敗すれば status は "failed" になる。env/command/args は常に「いま動いている値」を表すので、成功したときだけ pending_* の内容に更新し、pending_* を null に戻す。失敗したときは、Kubernetes の Deployment を元の値(env/command/args)に戻した上で、env/command/args も pending_* も変えず(試して失敗した値は pending_* に残る)、status だけを "failed" にする。利用者は pending_* を直して PUT し直すか、そのまま再度 apply するか、discard で破棄できる([デプロイ詳細取得](002-get-deploy-deployid.md) でポーリングして確認する)
- Note: 既に status が "applying" の場合(別のconfig/volumes/services applyが進行中、またはこのapply自体が既に実行中)は 409 を返す(多重apply不可)。"deleting" の間も 409 を返す
- Note: deploy の replicas が 0(status が "scaled_zero")のときは、再起動する Pod が無いのですぐに完了し、成功しても status は "active" ではなく "scaled_zero" に戻る。
- Note: applying には 300秒(5分) のタイムアウトを設ける。この時間内に起動完了(Ready)が確認できない場合、強制的に status を "failed" に遷移させる(再起動処理がハングしても "applying" のまま無期限に固まらないようにするため)

## 参照

- [dev-docs/endpoints/deploys.md](../../endpoints/deploys.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

# API トークンと webhook — 設計メモ

状態: **実装済み(通しの試験は、まだ)**。仕様は [docs/endpoints/api_tokens.md](endpoints/api_tokens.md)。

## 1. 目的

- 機械(CI、スクリプト)が、利用者の代わりに API を呼べるようにする。
- CI やレジストリからの **webhook で、deploy のイメージを更新して反映する**(自動デプロイ)。
- 同じ仕組みを、のちの「deploy 用のトークン」にも使う。

## 2. 決まったこと

| 項目 | 決定 |
|---|---|
| 置き場所 | **backend の中**(別のコンテナにはしない)。発行も検証も backend |
| 形式 | **JWT(HS512)**。検証では HS512 以外(HS256、alg=none)を拒否する。接頭辞 `lkt_` を付けて渡す(ログインの JWT と見分ける) |
| 署名の秘密 | 環境変数 `API_TOKEN_SECRET`(64 文字以上)。**空なら、機能は無効**(発行も使用も不可)。変えると、発行済みのトークンは全て使えなくなる |
| 許す操作 | claim `scope`: `read`(GET だけ)、`write`(全て)、`deploy:image`(イメージの更新だけ) |
| 対象の絞り込み | claim `deploys`、`projects`。どちらかを指定すると、パスの deploy かプロジェクトが範囲内のものだけを許す(対象を持たない操作 `POST /deploy` などは、拒否) |
| DB で管理するもの | **有効・無効だけ**(`api_tokens`。jti、持ち主、名前、期限、失効の日時、最終使用)。scope などの写しは、一覧の表示用 |
| 期限 | 発行時に日数で指定(省略は無期限) |
| webhook のトークンの渡し方 | **ヘッダー**(`X-Launchs-Token`、または `Authorization: Bearer`)。URL には入れない |
| 管理の API | **ログインした利用者だけ**(API トークンでは、発行も一覧も失効もできない) |

## 3. 認証の流れ

1. `/api/v1` の認証のミドルウェアが、トークンの接頭辞で振り分ける(`middlewares.NewTokenDispatchMiddleware`)。`lkt_` 以外は、これまでのログインの JWT。
2. API トークンは、署名と期限 → 失効(DB を 1 回引く。最終使用は 1 分に 1 回だけ更新) → 権限(`Claims.Authorize`。操作の種類と、対象)の順に確かめる。
3. 通ったら、持ち主のユーザーとして扱う。役割は、常に `user`(管理者の権限は与えない)。
4. 失効は、すぐに効く(キャッシュしない)。

## 4. イメージの更新

- `PUT /deploy/:id/image` `{imageRef, apply?}`。`deploys.pending_image_ref` に保存し、**起動設定の pending(env、command、args)も、現在の値のコピーで一緒に作る**。反映は、既存の `config/apply` と worker の Workflow が、そのまま行う(Deployment の組み立てだけ、pending のイメージを使う)。
- 起動設定の pending が、すでにあるときは、その内容を変えず、イメージだけを足す。
- 現在と同じイメージなら、何もしない。反映中・削除の途中・起動中は 409。
- 反映に失敗したら、既存どおり元に戻る(正式な値は変わらず、pending が残る)。`config/discard` で、イメージの pending も破棄する。

## 5. webhook

`POST /hooks/deploy`(`/api/v1` の外。nginx と Traefik の IngressRoute に、`/hooks/` を backend へ振り分ける設定がある)。

- トークンは、`deploy:image`(または `write`)で、**対象の deploy がちょうど 1 つ**のもの(画面の「自動デプロイ」が、この形で発行する)。
- ボディ(上から順に見る): `{"imageRef": "完全な参照"}`、`{"tag": "v2"}`(いまのイメージの、タグ(digest)だけを差し替える)、Docker Hub の `{"push_data": {"tag"}}`。
- 変更があれば、イメージを更新して、反映を始める(応答 `changed`、`applied`)。同じイメージなら何もしない(CI が、同じ内容で、何度呼んでも安全)。
- 反映中などは 409(呼び出し側で、あとから、やり直す)。

## 5.5 トークンの再表示と、GitHub Actions の YAML

- **webhook 用(`deploy:image`)のトークンだけ、あとから、もう一度見られる**(`GET /api-tokens/:id/reveal`)。本体は保存せず、保存してある内容から、同じ秘密で作り直す(HS512 は決定的)。権限の広いトークン(`read`、`write`)は、発行のときの 1 度だけ。
- deploy の画面の「GitHub Actions で自動デプロイ」が、3 ステップの案内(`frontend/components/github-actions-guide.tsx`)と、workflow(`frontend/lib/github-actions-workflow.ts`)を出す。
  1. **YAML を作る**: 対象のブランチ、ビルドするフォルダ、ビルドの方法(**Railpack**: `iloveitaly/github-action-railpack` / **Dockerfile**: `docker/build-push-action`)を決める → ダウンロードして、リポジトリの `.github/workflows/` に置いて push する手順。
  2. **Secrets を登録**: `LAUNCHS_URL` と `LAUNCHS_TOKEN` を、値のコピーつきで並べる(トークンは、その場で、発行・表示)。GitHub での登録のしかた(画面と `gh secret set`)。
  3. **ghcr を公開**: 最初の実行のあと、パッケージを Public にする手順。
- **動くとき**: 対象のブランチへの push(自動)と、Actions タブの「Run workflow」(手動。`workflow_dispatch`)。
- **YAML は、どのリポジトリに置いても動く**。イメージの名前は、YAML の中で、`ghcr.io/${GITHUB_REPOSITORY_OWNER,,}/launchs-<deploy の ID(deploy- なし)>` と作る(ghcr.io は小文字だけ。deploy ごとに違う名前なので、重ならない)。タグはコミットの ID。webhook には、`imageRef` で完全な参照を渡す。トークンと URL は、YAML に書かず、Secrets から読む。
- Railpack の action は `@master`(動く先が変わる)。YAML のコメントに、慣れたらコミットの ID に固定する、と書いてある。実際の GitHub Actions での通しは、まだ。

## 6. 画面

- アカウント設定の「API トークン」: 発行(名前、読み取り/全て、期間)、一覧、失効。トークンの本体は、発行したときに 1 度だけ見せる。
- deploy の「概要」の「自動デプロイ(webhook)」: その deploy 用のトークンの発行・失効、URL とヘッダー、`curl` の例。

## 7. 設定

- `API_TOKEN_SECRET`: `.env.example`、`docker-compose.yaml`、本番は Secret `launchs-secret`(`deploy/settings/secret.example.yaml`)。生成は `openssl rand -hex 64`。

## 8. これから

- 通しの試験(DB、Temporal、k8s を使うもの。イメージの更新、API トークンの認証、webhook)。
- GHCR など、他のレジストリの webhook の形式への対応(今は、`imageRef`、`tag`、Docker Hub)。
- deploy の画面に、webhook の最後の呼び出しと結果を出す。
- 秘密の更新(ローテーション)。いまは、1 つの秘密を替えると、全てのトークンが失効する。

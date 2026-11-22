# API トークンの管理と、自動デプロイ(webhook)

仕様は [api_tokens.md](../endpoints/api_tokens.md)、設計は [docs/api-tokens.md](../api-tokens.md)。

## トークンの管理(ログインしたユーザーだけ)

| メソッド | パス | 説明 |
|---|---|---|
| `POST` | `/api-tokens` | 発行。**本体(`token`)は、この応答でだけ返る** |
| `GET` | `/api-tokens` | 一覧(本体は含まない。最終使用、失効の状態つき) |
| `DELETE` | `/api-tokens/:tokenId` | 失効(すぐ効く。何度呼んでも 200) |

```bash
curl -sS -X POST $LAUNCHS/api/v1/api-tokens -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"ci","scope":["write"],"projects":["project-…"],"expiresInDays":90}'
```

| 項目 | 規則 |
|---|---|
| `name` | 必須、100 文字まで |
| `scope` | 1〜3 個。`read` / `write` / `deploy:image` |
| `deploys`、`projects` | 省略で、絞らない。50 個まで |
| `expiresInDays` | 1〜3650。省略で、無期限 |

画面からは、アカウント設定(全体用)、プロジェクトの「設定」(そのプロジェクトだけ)、deploy の「概要」(webhook 用)で、発行できる。

## 自動デプロイ(webhook)

CI が、新しいイメージを push したあと、1 回呼ぶだけで、deploy を入れ替える。

```bash
curl -fsS -X POST $LAUNCHS/hooks/deploy \
  -H "X-Launchs-Token: $LAUNCHS_TOKEN" \
  -d '{"tag":"v2"}'
# → {"deployId":"deploy-…","imageRef":"ghcr.io/org/app:v2","changed":true,"applied":true}
```

- **トークン**: `deploy:image`(または `write`)で、`deploys` が**ちょうど 1 つ**のもの。ヘッダー `X-Launchs-Token`(または `Authorization: Bearer`)で渡す。URL には入れない。
- **ボディ**(上から順に見る):

| ボディ | 意味 |
|---|---|
| `{"imageRef":"ghcr.io/org/app:v2"}` | 参照をまるごと指定 |
| `{"tag":"v2"}` | いまのイメージの、**タグ(と digest)だけ**を差し替える。ホストのポート(`localhost:5000/…`)を、タグと取り違えない |
| `{"push_data":{"tag":"v2"}}` | Docker Hub の webhook の形式 |

- **べき等**: 同じイメージなら、何もしない(`changed: false`、`applied: false`)。何度呼んでも安全。
- **反映中は 409**。あとから、やり直す(CI のリトライ向き)。
- **失敗したら、元のイメージのまま動き続ける。**
- 結果は、`GET /deploy/:deployId` の `status` と `imageRef` で確かめる。

| 状況 | 結果 |
|---|---|
| トークンが無い、不正、失効 | 401 |
| scope が足りない、対象の deploy が 1 つでない | 403 |
| イメージを決められない、参照に使えない文字 | 400 |
| 反映中、起動中、削除中 | 409 |

## トークンの再表示

webhook 用(`deploy:image`)のトークンは、deploy の「自動デプロイ」で、あとから**もう一度見られる**(`GET /api-tokens/:tokenId/reveal`。ログインしたユーザーだけ)。本体は保存せず、同じ秘密で、同じものを作り直す。`read`、`write` のトークンは、発行のときの 1 度だけ。

## CI の例

### GitHub Actions

deploy の画面の「**GitHub Actions で自動デプロイ**」が、3 ステップで案内する(① 対象のブランチ、ビルドするフォルダ、**Railpack / Dockerfile** を選んで YAML を作る → ダウンロードして push、② Secrets(`LAUNCHS_URL`、`LAUNCHS_TOKEN`)の登録、③ ghcr.io のパッケージを公開にする)。YAML は、push で自動で動き、Actions タブの「Run workflow」で手動でも動かせる。どのリポジトリに置いても動き、イメージの名前(`ghcr.io/<持ち主>/launchs-<deploy の ID>`)は、YAML の中で作る。手で書くなら、次のとおり。

```yaml
- name: launchs に反映
  run: |
    curl -fsS -X POST "${{ secrets.LAUNCHS_URL }}/hooks/deploy" \
      -H "X-Launchs-Token: ${{ secrets.LAUNCHS_TOKEN }}" \
      -d "{\"tag\":\"${{ github.sha }}\"}"
```

**タグは、毎回違うもの(コミットの ID など)にする。** `latest` のままでは、「変わった」と判断できない。

### 反映の完了まで待つ

```bash
curl -fsS -X POST $LAUNCHS/hooks/deploy -H "X-Launchs-Token: $TOKEN" -d "{\"tag\":\"$SHA\"}"
# webhook のトークンは、イメージの更新だけ。状態を見るには、read のトークンが別に要る
until [ "$(curl -fsS -H "Authorization: Bearer $READ_TOKEN" $LAUNCHS/api/v1/deploy/$DEPLOY | jq -r .status)" != applying ]; do sleep 3; done
```

## セキュリティ

- トークンは、パスワードと同じ。**CI のシークレットに入れる**。リポジトリやログに出さない。
- **必要なものだけに絞る**(webhook は `deploy:image` と deploy 1 つ。監視は `read`)。
- 漏れたら、すぐ失効する。期限を付けて、定期的に替える。
- サーバーの `API_TOKEN_SECRET` を替えると、**全ての API トークンが使えなくなる**。

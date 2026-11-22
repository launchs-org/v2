# launchs API ガイド(開発者向け)

launchs を、プログラム、CI、スクリプトから操作するためのガイド。画面でできることは、API でもできる。

> **仕様の正本は [docs/endpoints/](../endpoints/README.md)**(全項目、全エラー、細かい規則)。ここは、**使う側の入口**で、流れ、例、落とし穴を書く。利用者向けのやさしい説明は [guide/api.html](../../guide/api.html)。

## 目次

| ファイル | 内容 |
|---|---|
| [authentication.md](authentication.md) | 認証。API トークンと、ログインのトークン |
| [conventions.md](conventions.md) | 共通のルール。エラー、ページ送り、「保存 → 反映」、非同期の待ち方 |
| [projects.md](projects.md) | プロジェクト、イベント、タスク、クォータ |
| [deploys.md](deploys.md) | deploy(アプリの起動)、起動設定、スケール、イメージの更新、ログ |
| [networking.md](networking.md) | Service、Ingress(公開 URL) |
| [storage.md](storage.md) | ボリューム、ファイル、ConfigMap |
| [tags-metrics.md](tags-metrics.md) | タグ、一斉削除、メトリクス |
| [templates.md](templates.md) | テンプレートからの一括作成 |
| [webhooks.md](webhooks.md) | API トークンの管理、自動デプロイ(webhook)、CI の例 |

## 3 分で始める

```bash
# 1. 画面の「アカウント設定 → API トークン」で、トークンを発行する(本体は、発行のとき 1 度だけ見える)
export LAUNCHS=https://<ホスト>            # 例: https://172.30.1.1:18443
export TOKEN=lkt_...

# 2. 呼ぶ
curl -sS -H "Authorization: Bearer $TOKEN" $LAUNCHS/api/v1/projects

# 3. 作る(プロジェクト → deploy)
PROJECT=$(curl -sS -X POST $LAUNCHS/api/v1/projects -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"my-app"}' | jq -r .projectId)
curl -sS -X POST $LAUNCHS/api/v1/deploy -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"projectId\":\"$PROJECT\",\"imageRef\":\"nginx:1.27\",\"spec\":\"small\"}"
```

(自己署名の証明書のときは、`curl -k`。)

## 作りが分かると楽になること

1. **ID は接頭辞つきの文字列**(`project-…`、`deploy-…`、`volume-…`、`apitoken-…`)。中身は解釈しない。
2. **変更は「保存(pending)→ 反映(apply)」の 2 段階**。反映は非同期。→ [conventions.md](conventions.md)
3. **他の人の資源は、存在しても 404**(403 にはならない)。

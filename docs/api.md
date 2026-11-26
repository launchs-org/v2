---
title: "API と自動デプロイ"
nav: "API と自動デプロイ"
lead: "画面でできることは、API からも、できます。CI やスクリプトから操作したり、新しいイメージを作ったときに、自動で入れ替えたりできます。"
crumb: "API と自動デプロイ"
---

## はじめかた(3 ステップ) {#start}

:::steps
1. **トークンを作る** — [[アカウント設定]](または、プロジェクトの[[設定]])の[[API トークン]]で、[[トークンを発行]]を押します。**トークンは、発行したときの 1 度しか表示されません**。保存してください。

2. **呼んでみる** — `Authorization: Bearer <トークン>` を付けて、API を呼びます。

3. **要らなくなったら失効** — 一覧の[[失効]]で、すぐに使えなくなります。
:::

```
curl -H "Authorization: Bearer lkt_…" https://<このサービスのホスト>/api/v1/projects
```

## 認証: 2 種類のトークン {#auth}

|   | API トークン(`lkt_…`) | ログインのトークン |
|---|---|---|
| 使う場面 | **機械(CI、スクリプト)** | 画面(ブラウザ) |
| 有効期間 | 発行のときに決める(30 日・90 日・1 年・無期限) | 15 分(更新しながら使う) |
| できること | 権限と対象を、絞れる | あなたと同じ全て |
| 取り消し | 一覧から失効(すぐ効く) | ログアウト |

### API トークンの権限

| 権限 | できること |
|---|---|
| **読み取り専用** | 見るだけ(GET)。状態の監視向け |
| **全ての操作** | 作成・更新・削除も、あなたと同じ。**ただし、API トークンの管理は、できません**(トークンが、別のトークンを作れないように) |
| **イメージの更新だけ** | 1 つの deploy の、イメージの更新と、自動デプロイ(webhook)だけ。deploy の画面で発行します |

:::note tip 対象を絞る
プロジェクトの[[設定]]で発行したトークンは、**そのプロジェクトだけ**を操作できます。CI には、必要なものだけに絞ったトークンを渡してください。絞ったトークンでは、プロジェクトの外の操作(`GET /projects` の一覧や、`POST /deploy` での作成)は、まだできません。
:::

:::note warn トークンは、パスワードと同じ
リポジトリに書いたり、チャットに貼ったりしないでください。CI の「シークレット」に入れます。漏れたかもしれないときは、すぐ失効します。
:::

## 基本のルール {#rules}

- **URL** は `/api/v1/…`。本文は JSON(`Content-Type: application/json`)。
- **エラー**は `{"error": {"code": "not_found", "message": "…"}}` の形。主な `code` は、`invalid_request`(400)、`unauthorized`(401)、`forbidden`(403)、`not_found`(404)、`conflict`(409。反映中など)。
- **一覧**は `limit`(最大 100)と `cursor`(前回の `nextCursor`)で、ページをめくります。
- **変更は「保存 → 反映」の 2 段階**です。`PUT …/config` のような変更は、まず**反映待ち**として保存され、`POST …/apply` で、はじめて実際に反映されます(`POST …/discard` で破棄)。反映は**非同期**で、すぐに応答します。結果は、deploy の `status`(`applying` → `active` か `failed`)を、`GET /deploy/:deployId` で確かめます。
- 反映中(`applying`)の deploy への変更は、`409` になります。少し待って、やり直してください。

## API の一覧 {#endpoints}

パスは、`/api/v1` を省いて書いています。ここに無い詳細(項目、例)は、リポジトリの `docs/endpoints/` にあります。

### プロジェクト {#projects}

| メソッド | パス | 説明 |
|---|---|---|
| `GET` | `/projects` | 一覧 |
| `POST` | `/projects` | 作成 |
| `GET` | `/projects/:projectId` | 取得 |
| `PATCH` | `/projects/:projectId` | 名前の変更 |
| `DELETE` | `/projects/:projectId` | 削除(中のものも全て消えます) |
| `GET` | `/projects/:projectId/events` | イベント |
| `GET` | `/projects/:projectId/tasks` | タスク(操作の履歴) |
| `GET` | `/projects/:projectId/tasks/:taskId` | タスクの詳細 |
| `GET` | `/quota` | 自分の上限と使用量 |

### デプロイ(アプリの起動) {#deploys}

| メソッド | パス | 説明 |
|---|---|---|
| `POST` | `/deploy` | イメージから作成 |
| `GET` | `/projects/:projectId/deploys` | 一覧 |
| `GET` | `/deploy/:deployId` | 詳細(状態、設定、反映待ちの変更) |
| `DELETE` | `/deploy/:deployId` | 削除 |
| `POST` | `/deploy/:deployId/scale` | 台数の変更 |
| `PUT` | `/deploy/:deployId/image` | **イメージの更新**(`apply: true` で、続けて反映) |
| `GET` | `/deploy/:deployId/logs` | ログ |
| `GET` | `/deploy/:deployId/metrics` | CPU・メモリ |

### 起動設定・マウント・ファイル {#config}

| メソッド | パス | 説明 |
|---|---|---|
| `GET / PUT` | `/deploy/:deployId/config` | 環境変数・コマンド・引数(PUT は、反映待ちとして保存) |
| `POST` | `/deploy/:deployId/config/apply` | 反映する |
| `POST` | `/deploy/:deployId/config/discard` | 反映待ちを破棄 |
| `GET / PUT` | `/deploy/:deployId/mounts` | ボリュームのマウント(`/mounts/apply`、`/mounts/discard` も同じ) |
| `GET / PUT` | `/deploy/:deployId/files` | ファイル(`/files/apply`、`/files/discard` も同じ) |
| `GET / PUT` | `/deploy/:deployId/configmap-mounts` | ConfigMap のマウント(`/apply`、`/discard` も同じ) |

### Service・Ingress {#service}

| メソッド | パス | 説明 |
|---|---|---|
| `POST / GET / PATCH / DELETE` | `/deploy/:deployId/service` | Service の作成・取得・ポートの変更・削除(`/apply`、`/discard`) |
| `POST / GET` | `/projects/:projectId/ingresses` | Ingress(公開 URL)の作成・一覧 |
| `GET / PUT / PATCH / DELETE` | `/projects/:projectId/ingresses/:ingressId` | 取得・ルールの変更・アクセス集計の切り替え・削除(`/apply`、`/discard`) |
| `GET` | `/projects/:projectId/ingresses/:ingressId/access-stats` | アクセスの集計 |
| `GET` | `/projects/:projectId/ingresses/:ingressId/access-logs` | アクセスログ |
| `GET / POST` | `/projects/:projectId/ingresses/:ingressId/log-exports` | アクセスログの転送(webhook / Google Drive)の一覧・作成([アクセスログを外に残す](log-export.md)) |
| `PATCH / DELETE` | `/projects/:projectId/ingresses/:ingressId/log-exports/:logExportId` | 転送の変更(有効・無効、URL、間隔)・削除(`/test` でテスト送信) |
| `GET / DELETE` | `/account/google-drive` | アカウントの Google Drive の接続の状態・解除(`/authorize` で接続の URL) |

### ボリューム・ConfigMap {#storage}

| メソッド | パス | 説明 |
|---|---|---|
| `POST / GET` | `/projects/:projectId/volumes` | 作成・一覧 |
| `GET / PATCH / DELETE` | `/projects/:projectId/volumes/:volumeId` | 取得・名前の変更・削除 |
| `PATCH` | `/projects/:projectId/volumes/:volumeId/size` | 大きさの変更(`/apply`、`/discard`) |
| `POST / GET` | `/projects/:projectId/configmaps` | ConfigMap の作成・一覧 |
| `GET / PUT / PATCH / DELETE` | `/configmaps/:configMapId` | 取得・新しい版・名前の変更・削除 |
| `GET` | `/configmaps/:configMapId/revisions` | 版の一覧 |
| `GET` | `/configmaps/:configMapId/usage` | どの deploy が使っているか |

### タグ・メトリクス・テンプレート {#others}

| メソッド | パス | 説明 |
|---|---|---|
| `PUT` | `/deploy/:deployId/tags` | deploy のタグ(volume、service、ingress も、同じ形) |
| `GET` | `/projects/:projectId/tags` | タグの一覧 |
| `DELETE` | `/projects/:projectId/tags/:tag/deploys` | タグのものを、まとめて削除 |
| `GET` | `/projects/:projectId/metrics` | プロジェクトのメトリクス |
| `GET` | `/projects/:projectId/tags/:tag/metrics` | タグのメトリクス |
| `GET` | `/templates` | テンプレートの一覧(`/templates/:templateId` で詳細) |
| `POST` | `/projects/:projectId/templates/:templateId/apply` | テンプレートから、まとめて作成 |

### API トークン {#tokens}

| メソッド | パス | 説明 |
|---|---|---|
| `POST` | `/api-tokens` | 発行(**ログインした人だけ**) |
| `GET` | `/api-tokens` | 一覧 |
| `DELETE` | `/api-tokens/:tokenId` | 失効 |

## 自動デプロイ(webhook) {#autodeploy}

新しいイメージを作ったときに、CI から 1 回呼ぶだけで、deploy のイメージを入れ替えて、反映します。

:::steps
1. deploy の[[概要]]にある[[自動デプロイ(webhook)]]で、[[トークンを発行]]を押します。(この deploy の、イメージの更新だけができるトークンです)

2. CI のシークレットに、トークンを入れます。

3. イメージを push したあと、次を呼びます。
:::

```
curl -X POST https://<このサービスのホスト>/hooks/deploy \
  -H "X-Launchs-Token: $LAUNCHS_TOKEN" \
  -d '{"tag":"v2"}'
```

| 本文 | 意味 |
|---|---|
| `{"tag":"v2"}` | いまのイメージの、**タグだけ**を差し替える(`ghcr.io/org/app:v1` → `…:v2`) |
| `{"imageRef":"ghcr.io/org/app:v2"}` | イメージを、まるごと指定する |
| Docker Hub の webhook の形式 | そのまま使えます(`push_data.tag`) |

- 応答は `{"changed": true, "applied": true}`。**同じイメージなら、何も起きません**(`changed: false`)。CI が、何度呼んでも安全です。
- 反映中は `409` です。少し待って、もう一度呼んでください。
- 反映に失敗したら、元のイメージのままで、動き続けます。
- タグに `latest` だけを使うと、「変わった」と判断できません。**コミットの ID など、毎回違うタグ**を付けてください。

### GitHub Actions の例

deploy の[[自動デプロイ(webhook)]]の[[GitHub Actions で自動デプロイ]]を押すと、4 つの手順で案内されます(くわしくは [GitHub との連携](github.md))。

:::steps
1. **YAML を作る**

   対象のブランチ、ビルドするフォルダ、ビルドの方法(**Railpack**=Dockerfile なし、または **Dockerfile**)を選ぶと、YAML ができます。

2. **GitHub に置く**

   ダウンロードして、リポジトリの `.github/workflows/` に置き、push します。どのリポジトリに置いても動きます。

3. **Secrets を登録**

   GitHub の **Settings → Secrets and variables → Actions** に、`LAUNCHS_URL` と `LAUNCHS_TOKEN` を登録します(値は、画面からコピーできます)。

4. **ghcr を公開**

   最初の実行のあと、パッケージを **Public** にします。
:::

作った workflow は、対象のブランチへの **push で自動**で動き、リポジトリの **Actions** タブの [[Run workflow]] からは**手動**でも動かせます。トークンは、YAML ではなく、Secrets に入れます。発行済みのトークンは、[[トークンを表示]]で、もう一度見られます。

:::note warn ghcr.io を使うとき
GitHub のパッケージは、最初は**非公開**です。launchs は、非公開のイメージを取得できないので、GitHub の Packages の設定で、**公開(Public)**にしてください。
:::

```
- name: launchs に反映
  run: |
    curl -fsS -X POST "${{ secrets.LAUNCHS_URL }}/hooks/deploy" \
      -H "X-Launchs-Token: ${{ secrets.LAUNCHS_TOKEN }}" \
      -d "{\"tag\":\"${{ github.sha }}\"}"
```

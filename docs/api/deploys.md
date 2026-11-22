# deploy(アプリの起動)

イメージから、k8s の Deployment を作る。仕様は [deploys.md](../endpoints/deploys.md)。

## 作る

```bash
curl -sS -X POST $LAUNCHS/api/v1/deploy -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"projectId":"project-…","imageRef":"nginx:1.27","spec":"small"}'     # → {"deployId":"deploy-…"}
```

- `spec` は `small` / `medium` / `large`(CPU とメモリ。**作成後は変えられない**)。
- `imageRef` は 512 文字まで。**公開されたイメージのみ**(非公開のレジストリは、まだ)。
- 5 分で起動しないと、自動で失敗になり、作ったものは片づけられる。
- クォータ超過は 409。

## 操作の一覧

| メソッド | パス | 説明 |
|---|---|---|
| `GET` | `/projects/:projectId/deploys` | 一覧 |
| `GET` | `/deploy/:deployId` | 詳細(状態、Pod、設定、反映待ち、Service) |
| `DELETE` | `/deploy/:deployId` | 削除 |
| `POST` | `/deploy/:deployId/scale` | 台数 `{"replicas": 0}`(0〜5。0 で停止) |
| `GET` / `PUT` | `/deploy/:deployId/config` | 環境変数、コマンド、引数 |
| `POST` | `/deploy/:deployId/config/apply` / `discard` | 反映 / 破棄 |
| `PUT` | `/deploy/:deployId/image` | **イメージの更新** |
| `GET` / `PUT` | `/deploy/:deployId/mounts` | ボリュームのマウント(`apply` / `discard`) |
| `GET` / `PUT` | `/deploy/:deployId/files` | ファイル(`apply` / `discard`) |
| `GET` / `PUT` | `/deploy/:deployId/configmap-mounts` | ConfigMap のマウント(`apply` / `discard`) |
| `GET` | `/deploy/:deployId/logs` | ログ(`logPods` の Pod を指定) |
| `GET` | `/deploy/:deployId/metrics` | CPU・メモリ(`range=1h/6h/24h`) |

## 起動設定を変える

```bash
# 環境変数・コマンド・引数は、常に 3 つまとめて、全件を送って置き換える
curl -sS -X PUT $LAUNCHS/api/v1/deploy/$DEPLOY/config -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"env":{"PORT":"8080"},"command":null,"args":null}'
curl -sS -X POST $LAUNCHS/api/v1/deploy/$DEPLOY/config/apply -H "Authorization: Bearer $TOKEN"
```

`env` は必須(空なら `{}`)。`command`、`args` は `null` で、イメージの既定。

## イメージを更新する

```bash
curl -sS -X PUT $LAUNCHS/api/v1/deploy/$DEPLOY/image -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"imageRef":"ghcr.io/org/app:v2","apply":true}'                      # → {"changed":true,"applied":true}
```

- `apply` を省くと、**反映待ちとして保存するだけ**(`pending_image_ref`)。あとで `config/apply` で反映する。
- **起動設定の反映待ちと同じ流れ**。すでに起動設定の反映待ちがあれば、その内容に、イメージを足して、一緒に反映する。
- **いまと同じイメージなら、何もしない**(`changed: false`)。何度呼んでも安全。
- 反映中・起動中・削除中は 409。
- 反映に失敗したら、元のイメージで動き続ける。`config/discard` で、反映待ちを捨てる。
- 自動化するなら → [webhooks.md](webhooks.md)

## 落とし穴

- `latest` のようなタグは、同じ参照のままなので「変わった」と判断できず、**更新されない**。コミットの ID など、毎回違うタグを使う。
- `failed` は、**元の値で動いている**ことが多い。`status` だけで、止まっていると思わない(`pods` と `logs` も見る)。
- 台数が 0 のとき、`status` は `scaled_zero`(失敗ではない)。落ちては立ち上がる状態が 10 分続くと、自動で 0 になる。

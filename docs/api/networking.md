# Service と Ingress(公開 URL)

## Service(deploy への内部の入口)

deploy ごとに 1 つ。クラスターの中から、名前でつなぐためのもの。仕様は [services.md](../endpoints/services.md)。

| メソッド | パス | 説明 |
|---|---|---|
| `POST` / `GET` / `DELETE` | `/deploy/:deployId/service` | 作成 / 取得 / 削除 |
| `PATCH` | `/deploy/:deployId/service` | ポートの変更(反映待ちとして保存) |
| `POST` | `/deploy/:deployId/service/apply` / `discard` | 反映 / 破棄 |
| `PUT` | `/deploy/:deployId/service/tags` | タグ |

## Ingress(インターネットへの公開)

ホスト名は、**自動で払い出される**(`形容詞-色-動物.ベースドメイン`)。**HTTP のみ**。仕様は [ingresses.md](../endpoints/ingresses.md)。

| メソッド | パス | 説明 |
|---|---|---|
| `POST` / `GET` | `/projects/:projectId/ingresses` | 作成 / 一覧 |
| `GET` / `DELETE` | `/projects/:projectId/ingresses/:ingressId` | 取得 / 削除 |
| `PUT` | `/projects/:projectId/ingresses/:ingressId` | ルールの変更(反映待ち) |
| `POST` | `…/:ingressId/apply` / `discard` | 反映 / 破棄 |
| `PATCH` | `…/:ingressId` | アクセス集計の切り替え |
| `GET` | `…/:ingressId/access-stats` | アクセスの集計(`range`) |
| `GET` | `…/:ingressId/access-logs` | アクセスログ(新しいものから。保存は短い) |
| `PUT` | `…/:ingressId/tags` | タグ |

## 公開までの流れ

1. deploy を作る → `active` を待つ。
2. Service を作る(deploy のポートを決める)。
3. Ingress を作り、ルール(パスと、Service)を `PUT` → `apply`。
4. 返ってきたホスト名で、アクセスする。

## 落とし穴

- Ingress は、Service が `active` のものにつなぐ。**Service を消すと、Ingress が指す先が無くなる**。
- Service のポートを変えたら、Ingress のルールも合っているかを確かめる。

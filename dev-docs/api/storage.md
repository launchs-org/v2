# ボリューム、ファイル、ConfigMap

## ボリューム(消えないデータ)

k8s の PVC。**最初にマウントを反映するときに、実際に作られる**。仕様は [volumes.md](../endpoints/volumes.md)。

| メソッド | パス | 説明 |
|---|---|---|
| `POST` / `GET` | `/projects/:projectId/volumes` | 作成 / 一覧 |
| `GET` / `PATCH` / `DELETE` | `/projects/:projectId/volumes/:volumeId` | 取得 / 名前の変更 / 削除 |
| `PATCH` | `…/:volumeId/size` | 大きさの変更(**拡張のみ**。反映待ち。`apply` / `discard`) |
| `GET` / `PUT` | `/deploy/:deployId/mounts` | deploy へのマウント(`apply` / `discard`) |
| `PUT` | `…/:volumeId/tags` | タグ |

- 大きさの上限は、サーバーの設定(`VOLUME_MAX_SIZE_MB`)と、クォータ。
- **一括削除(タグ)では、ボリュームは消えない**(データを守るため)。個別に消す。

## ファイル(deploy 専用の設定ファイル)

`GET` / `PUT /deploy/:deployId/files`(`apply` / `discard`)。deploy の中に、ファイルを置く。1 つの deploy のためのもの。

## ConfigMap(プロジェクトで共有する設定ファイルの組)

仕様は [configmaps.md](../endpoints/configmaps.md)。

| メソッド | パス | 説明 |
|---|---|---|
| `POST` / `GET` | `/projects/:projectId/configmaps` | 作成 / 一覧 |
| `GET` / `PUT` / `PATCH` / `DELETE` | `/configmaps/:configMapId` | 取得 / **新しい版を足す** / 名前の変更 / 削除 |
| `GET` | `/configmaps/:configMapId/revisions` | 版の一覧 |
| `GET` | `/configmaps/:configMapId/usage` | どの deploy が使っているか |
| `GET` / `PUT` | `/deploy/:deployId/configmap-mounts` | deploy へのマウント(`apply` / `discard`) |

- テキストと base64 のバイナリ。**1 MiB まで**。
- **編集は、新しい版を足すだけ**。deploy は、**版を固定して**マウントする(勝手に変わらない)。新しい版を使うには、マウントの版を上げて `apply` する。
- 使われている ConfigMap の削除は、拒否される(`usage` で確かめる)。

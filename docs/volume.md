# volume(永続ストレージ)

プロジェクトの永続ストレージ。`/projects/:projectId/volumes` の API で作り、`/deploy/:deployId/mounts` で deploy にマウントする。k8s の PVC 1つに対応する。
API の仕様は [endpoints/volumes.md](endpoints/volumes.md)、呼び出しの流れは [call-flow.md](call-flow.md) の 5.8。

## 仕組み

- **作成**: volume の行を作るだけ(status は `active`)。PVC は作らない。プロジェクトの namespace は、最初の deploy の作成で作られるので、volume の作成の時点では、無いことがある。
- **PVC の作成**: 最初にマウントを適用するとき、namespace を用意してから、volume の現在の `sizeMB` で作る(すでにあれば、そのまま)。deploy を消しても、PVC とデータは残り、別の deploy にマウントし直せる。
- **拡張**: PVC の要求を広げ、PVC の実際の容量(`status.capacity`)が増えるのを、5分まで待つ。PVC がまだ無いときは、何もせず成功する。PVC は縮められないので、失敗しても巻き戻さない(`sizeMB` は動いている値のまま、`pending_sizeMB` は残る)。同じ値での再度の apply は、問題ない。
- **削除**: マウント中(まだ適用していないマウントも含む)の volume は、409。PVC を削除してから、行を削除する。
- **プロジェクトの削除**: namespace の削除で、PVC(とデータ)が消える。volume の行も消える。

## 設定(環境変数)

| 変数 | どこで読む | 既定 | 意味 |
|---|---|---|---|
| `VOLUME_MAX_SIZE_MB` | backend | 5120 | volume 1つの大きさの上限(MB)。アカウントごとの上限は、認証のコンテナを作るときに設ける |
| `VOLUME_STORAGE_CLASS` | worker | (空) | PVC の StorageClass。空なら、クラスターの既定。**拡張に対応したものが前提** |
| `VOLUME_ACCESS_MODE` | worker | `ReadWriteMany` | PVC のアクセスモード(`ReadWriteMany` か `ReadWriteOnce`)。作成済みの PVC には効かない(k8s がアクセスモードの変更を許さない) |

## 利用者が知っておくこと

- 大きさの単位は、MB と書いているが、実際は MiB で PVC を作る。
- **既定は ReadWriteMany**(複数のノードの Pod から、同時に使える。replicas を 2 以上にしても、deploy をまたいで同じ volume をマウントしても動く)。StorageClass が RWX に対応している必要がある(Longhorn は、NFS で提供する。ノードに NFS クライアントが要る)。書き込みの排他は、利用者のアプリが行う。
- `VOLUME_ACCESS_MODE=ReadWriteOnce` にすると、volume は 1つのノードにしか付けられない。replicas を 2 以上にして、Pod が別のノードに置かれると、2つ目以降の Pod が起動できない。
- マウントを持つ deploy は、更新のとき、古い Pod を止めてから新しい Pod を起動する(Recreate)。マウントの適用の間は、短く止まる。
- コンテナのユーザーが root でないとき、ボリュームに書けないことがある(所有者は、StorageClass とイメージ次第)。fsGroup は設定していない。

## 本番の RBAC

worker に、`persistentvolumeclaims` の get / create / update / delete を許す。

## 開発用のクラスター(Talos)での確認

StorageClass `longhorn`(拡張に対応)で確認した。

```
VOLUME_STORAGE_CLASS=longhorn task up:k8s
```

確認した流れ: volume を作る(PVC は無い)→ マウントを適用(PVC が `Bound`、Deployment が Recreate)→ ファイルを書く → マウント中の削除は 409 →
拡張して apply(PVC の要求と容量が 2Gi、Pod の中の `df` も増える、ファイルは残る)→ マウントを外す → 別の path に付け直すと、ファイルが残っている →
外して volume を削除(PVC とレコードが消える)→ プロジェクトを削除。

ReadWriteMany(既定)は、replicas を 3 にして、3つのノードに Pod を置き、1つの Pod が書いたファイルを、全ての Pod が読めることも確認した。

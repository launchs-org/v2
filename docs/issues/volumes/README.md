[← Issues 一覧](../README.md)

# ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)

- [ボリューム作成](001-post-projects-projectid-volumes.md) `Post /projects/:projectId/volumes`
- [ボリューム一覧](002-get-projects-projectid-volumes.md) `Get /projects/:projectId/volumes`
- [ボリューム取得](003-get-projects-projectid-volumes-volumeid.md) `Get /projects/:projectId/volumes/:volumeId`
- [ボリューム名前変更](004-patch-projects-projectid-volumes-volumeid.md) `Patch /projects/:projectId/volumes/:volumeId`
- [ボリューム拡張 (pendingとして保存)](005-patch-projects-projectid-volumes-volumeid-size.md) `Patch /projects/:projectId/volumes/:volumeId/size`
- [ボリューム 適用](006-post-projects-projectid-volumes-volumeid-apply.md) `Post /projects/:projectId/volumes/:volumeId/apply`
- [ボリューム 破棄](007-post-projects-projectid-volumes-volumeid-discard.md) `Post /projects/:projectId/volumes/:volumeId/discard`
- [ボリューム削除](008-delete-projects-projectid-volumes-volumeid.md) `Delete /projects/:projectId/volumes/:volumeId`
- [デプロイ ボリュームマウント取得](009-get-deploy-deployid-mounts.md) `Get /deploy/:deployId/mounts`
- [デプロイへのボリュームマウント設定](010-put-deploy-deployid-mounts.md) `Put /deploy/:deployId/mounts`
- [デプロイ ボリュームマウント 適用](011-post-deploy-deployid-mounts-apply.md) `Post /deploy/:deployId/mounts/apply`
- [デプロイ ボリュームマウント 破棄](012-post-deploy-deployid-mounts-discard.md) `Post /deploy/:deployId/mounts/discard`

## 備考

- ボリュームの `sizeMB` にはアカウントごとの上限(デフォルト 5GB = 5120MB)がある。拡張(005〜007)は拡大のみ許可し、縮小は不可。上限の引き上げは申請制で、申請フロー自体は別途アカウント管理側のグループで設計する。
- ボリュームの実体は、環境変数で選ぶ StorageClass の PVC(拡張に対応した StorageClass が前提)。`status` は "applying" | "active" | "failed" | "deleting"。削除は非同期で、`deleting` の間の変更系操作は 409。
- ボリュームの `name` 変更(004)はストレージの実体に影響しないメタデータ変更のため、他グループのような pending_*→apply/discard モデルは使わず即時反映する。`sizeMB` の拡張(005〜007)は実際のストレージのリサイズを伴うため、他グループと同様 pending_*→apply/discard モデルで扱う。

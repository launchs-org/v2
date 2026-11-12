# Issues 一覧

グループごとにフォルダを分けて管理する。各グループの詳細は以下を参照。

- [認証関連](auth/README.md) (/auth/)
- [プロジェクト関連](projects/README.md) (/projects)
- [ビルド関連](builds/README.md) (/upload, /build)
- [デプロイ・起動設定関連](deploys/README.md) (/deploy)
- [サービス関連](services/README.md) (/deploy/:deployId/service)
- [イングレス関連](ingresses/README.md) (/projects/:projectId/ingresses)
- [ボリューム関連](volumes/README.md) (/projects/:projectId/volumes, /deploy/:deployId/mounts)

## レビュー進捗

各グループのレビューは完了している。そのあと、実装前の確認で決まった内容(共通ルール、`deleting` 状態、ページング、トークンの仕様など。経緯は [実装前の確認項目](../pre-implementation-checklist.md))を全グループに反映した。反映後の再レビューは、省略した。

- [x] 認証関連 (/auth/) — レビュー完了(決定事項を反映済み)
- [x] プロジェクト関連 (/projects) — レビュー完了(決定事項を反映済み)
- [x] ビルド関連 (/upload, /build) — レビュー完了(決定事項を反映済み)
- [x] デプロイ・起動設定関連 (/deploy) — レビュー完了(決定事項を反映済み)
- [x] サービス関連 (/deploy/:deployId/service) — レビュー完了(決定事項を反映済み)
- [x] イングレス関連 (/projects/:projectId/ingresses) — レビュー完了(決定事項を反映済み)
- [x] ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts) — レビュー完了(決定事項を反映済み)

決まっていない細かい点は、[実装前の確認項目](../pre-implementation-checklist.md) の「反映の過程で出た、決まっていないこと」にまとめてある。

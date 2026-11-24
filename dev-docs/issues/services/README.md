[← Issues 一覧](../README.md)

# サービス関連 (/deploy/:deployId/service)

1 deploy = 1 service。1つのserviceが複数ポート(`ports`配列、各ポートごとにtcp/udpを選択)を公開する。

- [サービス作成 (ポート公開)](001-post-deploy-deployid-service.md) `Post /deploy/:deployId/service`
- [サービス取得](002-get-deploy-deployid-service.md) `Get /deploy/:deployId/service`
- [サービス更新 (一括置き換え、pendingとして保存)](003-patch-deploy-deployid-service.md) `Patch /deploy/:deployId/service`
- [サービス 適用](004-post-deploy-deployid-service-apply.md) `Post /deploy/:deployId/service/apply`
- [サービス 破棄](005-post-deploy-deployid-service-discard.md) `Post /deploy/:deployId/service/discard`
- [サービス削除](006-delete-deploy-deployid-service.md) `Delete /deploy/:deployId/service`

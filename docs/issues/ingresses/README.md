[← Issues 一覧](../README.md)

# イングレス関連 (/projects/:projectId/ingresses)

HTTPのみ対応。path ruleは serviceId + port(対象serviceのportsのうちprotocolがtcpのもの)を指定する。rulesはconfig/service/volumeと同様、pending_rules→apply/discardモデルで一括管理する(個別rule単位のCRUDエンドポイントは無い)。

- [イングレス作成 (ランダムドメイン発行)](001-post-projects-projectid-ingresses.md) `Post /projects/:projectId/ingresses`
- [イングレス一覧](002-get-projects-projectid-ingresses.md) `Get /projects/:projectId/ingresses`
- [イングレス取得](003-get-projects-projectid-ingresses-ingressid.md) `Get /projects/:projectId/ingresses/:ingressId`
- [イングレス削除](004-delete-projects-projectid-ingresses-ingressid.md) `Delete /projects/:projectId/ingresses/:ingressId`
- [イングレス更新 (rules一括置き換え、pendingとして保存)](005-put-projects-projectid-ingresses-ingressid.md) `Put /projects/:projectId/ingresses/:ingressId`
- [イングレス 適用](006-post-projects-projectid-ingresses-ingressid-apply.md) `Post /projects/:projectId/ingresses/:ingressId/apply`
- [イングレス 破棄](007-post-projects-projectid-ingresses-ingressid-discard.md) `Post /projects/:projectId/ingresses/:ingressId/discard`

# エンドポイント一覧

## 概要
全てのエンドポイントをグループごとに分けてまとめる

## 共通ルール
- [共通ルール](common.md) — パス、ID、日時、名前、エラー形式、認可、排他制御と削除、ページング

## グループ一覧
- [認証関連](auth.md) (/auth/)
- [アカウント管理(管理者)](auth_admin.md) (/auth/admin/)
- [プロジェクト関連](projects.md) (/projects)
- [ビルド関連](builds.md) (/upload, /build)
- [デプロイ・起動設定関連](deploys.md) (/deploy)
- [サービス関連](services.md) (/deploy/:deployId/service)
- [イングレス関連](ingresses.md) (/projects/:projectId/ingresses)
- [ボリューム関連](volumes.md) (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- [ConfigMap 関連](configmaps.md) (/projects/:projectId/configmaps, /configmaps/:configMapId)
- [クォータ(アカウントごとの上限)](quota.md) (/quota)
- [テンプレート](templates.md) (/templates)
- [タスク(Workflow の履歴)](tasks.md) (/projects/:projectId/tasks)
- [タグとメトリクス](tags.md) (/deploy/:deployId/tags、/projects/:projectId/tags、…/metrics)
- [API トークンと webhook](api_tokens.md) (/api-tokens、/hooks/deploy、/deploy/:deployId/image)

## 関連
- [Issues 一覧](../issues/README.md) — 各エンドポイントの詳細設計レビュー用Issue

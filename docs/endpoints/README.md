# エンドポイント一覧

## 概要
全てのエンドポイントをグループごとに分けてまとめる

## 共通ルール
- [共通ルール](common.md) — パス、ID、日時、名前、エラー形式、認可、排他制御と削除、ページング

## グループ一覧
- [認証関連](auth.md) (/auth/)
- [プロジェクト関連](projects.md) (/projects)
- [ビルド関連](builds.md) (/upload, /build)
- [デプロイ・起動設定関連](deploys.md) (/deploy)
- [サービス関連](services.md) (/deploy/:deployId/service)
- [イングレス関連](ingresses.md) (/projects/:projectId/ingresses)
- [ボリューム関連](volumes.md) (/projects/:projectId/volumes, /deploy/:deployId/mounts)

## 関連
- [Issues 一覧](../issues/README.md) — 各エンドポイントの詳細設計レビュー用Issue

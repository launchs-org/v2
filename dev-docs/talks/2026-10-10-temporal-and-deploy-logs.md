# 2026-10-10 技術調査: Temporal採用検討 / デプロイログ取得の追加

## 背景・経緯

全7グループのIssueレビュー完了後、実装に向けた技術調査フェーズとして、裏側のワークフロー基盤と、運用時に必要な機能(ログ参照)を検討した。

## Temporalの採用検討

- バックエンドの各種操作(ビルド、デプロイのapply、config/service/volume/ingressのapply等)を Temporal のワークフローとして実行する案が出た。
- これまでの設計(pending_*→apply/discardモデル、applyのタイムアウト、ビルドキャンセルの競合処理)は、いずれも「長時間実行される非同期処理+状態のポーリング」という構造を持っており、Temporalが解決する問題にちょうど合致すると判断。
  - pending→apply/discardの一連の流れ → Workflow + Activity で自然に表現できる
  - ビルドキャンセルの競合(完了が先行したらキャンセル不成立) → Temporalのsignal/cancellationの仕組みで素直に書ける
  - applyのタイムアウト(300秒/60秒) → Workflowのtimeoutでそのまま表現可能
  - リトライや一時的なインフラ障害への対応 → Temporal標準機能でカバー
- 検討が必要な論点(未確定、実装フェーズで詰める):
  - API の GET は DB の状態を返す設計なので、Workflow の実行結果をどうDBへ同期するか(Activity経由の書き込みが必要)
  - 409多重apply不可の排他制御を、Workflow ID の一意性(deployIdベース)で表現できるか
  - Temporal Server自体の運用コスト(オンプレk8s環境への追加構築)

## デプロイログ取得の追加

- 「障害時の切り分けをやりやすくするため、各deployのランタイムログを見れるようにしたい」という要望から、新しいエンドポイントを追加。
- [ビルドログ取得](../issues/builds/004-get-build-buildid-logs.md)と同じ `since`/`nextSince` のポーリング形式を踏襲することで合意。
- deploy は複数 replica(Pod)を持ち得るため、「どのPodのログを返すか」という論点が発生。結論:
  - [デプロイ詳細取得](../issues/deploys/002-get-deploy-deployid.md) のレスポンスに `pods` 配列(各Podの `podId`/`status`)を追加。既存の `replicas`(希望レプリカ数、数値)とは別フィールドにして後方互換を保った。
  - 新設の [デプロイ ログ取得](../issues/deploys/009-get-deploy-deployid-logs.md)(`GET /deploy/:deployId/logs`)で、`podId` を必須クエリとして指定し、その Pod 1つ分のログを返す。全replica分の集約は今回は行わない(必要なら podId を変えて複数回呼ぶ)。
  - Pod が再起動されると podId が変わり、古い podId のログは取得できなくなる点は仕様として明記。恒久的なログ保管(別途ログ収集基盤)は今回のスコープ外とした。
- `dev-docs/endpoints/deploys.md`(正本)と `dev-docs/issues/deploys/002`・新設`009`に反映済み。

## 影響を受けたファイル

- `dev-docs/endpoints/deploys.md`: `pods` フィールド追加、「デプロイ ログ取得」セクション追加
- `dev-docs/issues/deploys/002-get-deploy-deployid.md`: `pods` 関連のテスト要件・仕様を追記
- `dev-docs/issues/deploys/009-get-deploy-deployid-logs.md`: 新規作成
- `dev-docs/issues/deploys/README.md`: 索引に追加

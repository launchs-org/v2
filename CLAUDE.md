# CLAUDE.md

このリポジトリで作業する際の案内。

## プロジェクト概要

launchs-org 用に、Kubernetes 上でのデプロイ管理を行うバックエンド/フロントエンドを作成するプロジェクト。詳細は [readme.md](readme.md) を参照。

- バックエンド: Golang + Echo + Gorm(PostgreSQL)
- フロントエンド: 未定
- インフラ: 開発環境 Podman(Docker)、本番環境 Kubernetes(オンプレ)

## ディレクトリ構成

```
backend/        バックエンド(Golang/Echo)のソース一式
docs/
  endpoints/    確定したエンドポイント仕様(実装のソースオブトゥルース)
  issues/       エンドポイントごとの詳細設計レビュー用Issue
docker-compose.yaml  開発環境用
```

## docs/endpoints/ — エンドポイント仕様

全エンドポイントの確定仕様(Method/Path/Header/Body/Response/Note)をまとめたドキュメント。

- 元々 `docs/endpoints.md` 1ファイルにまとめていたが、ボリュームが大きくなりすぎたため、グループ(認証・プロジェクト・ビルド・デプロイ・サービス・イングレス・ボリューム)ごとにファイルを分割した。
- [docs/endpoints/README.md](docs/endpoints/README.md) が索引。各グループの仕様ファイルへのリンクを持つ。
- ここに書かれている内容が実装すべき仕様の正本。pending_* → apply/discard という非同期反映モデル(config/service/ingress/volumeの変更はまず pending として保存し、apply で初めて実際に反映される)が全体を通した設計方針なので、新しいエンドポイントを追記する際もこのパターンに合わせる。

## docs/issues/ — Issue管理・レビュー

`docs/endpoints/` で定義した各エンドポイントについて、実装前にテスト要件(正常系・異常系・境界値)まで含めて1本ずつ詳細設計レビューを行うための Issue 置き場。

- 以前は全エンドポイントの Issue をフラットな連番ファイルで管理していたが、見通しが悪くなったため、`docs/endpoints/` と同じグループ単位でサブディレクトリを切り直した(例: `docs/issues/auth/`, `docs/issues/projects/`)。
- [docs/issues/README.md](docs/issues/README.md) が全体の索引であり、**グループごとのレビュー進捗チェックリストも兼ねる**。新しいグループのIssueを作る/完了させたら、このチェックリストを更新すること。
- 各グループフォルダには `README.md`(そのグループ内Issue一覧)と、連番付きの個別Issue(`001-post-xxx.md` 等)を置く。
- 個別Issueのフォーマットは以下の構成で統一する(詳細は既存ファイルを参照):
  1. カテゴリ / Method・Path
  2. 説明
  3. テスト要件(正常系・異常系・境界値)
  4. 仕様(`docs/endpoints/` の該当箇所をベースにしたJSON例など)
  5. 参照(該当する `docs/endpoints/*.md` へのリンク)
- **レビューは1グループずつ進める**。ユーザーが現在のグループのレビューを完了したと明言するまで、次のグループのIssueは作成しない。

## 作業時の注意

- `docs/endpoints/*.md` と `docs/issues/**/*.md` の間は相互にリンクを張っているため、ファイルを移動・リネームする際は相対パスのリンク切れがないか確認する。
- git 履歴の扱いについて: develop ブランチは一度 force push で過去の Issue 関連コミットを整理し直した経緯がある。force push や reset --hard など履歴を書き換える操作は、必ず事前にユーザーの明示的な承認を取ってから行う。

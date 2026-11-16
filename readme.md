# lacunsh-org を書き直す

## 概要
Launchs-org 用に Kubernetes の操作やデプロイの管理を行うバックエンドとフロントエンドを作成する


## 技術構成
### フロントエンド
- React + Vite + TypeScript(SPA。詳細は docs/frontend.md)

### バックエンド
- 言語: Golang
- ライブラリ: Echo
- データベース: Gorm(Postgresql)

### インフラ
- 開発環境: Podman(Docker)
- 本番環境: Kubernetes(オンプレ)
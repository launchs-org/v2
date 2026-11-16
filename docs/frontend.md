# フロントエンド(`frontend/`)

React + Vite + TypeScript の SPA。バックエンドの API(`/api/v1/...`)と、認証(`/auth/...`)を、同じオリジンから呼ぶ(CORS は要らない)。

## 決まっていること

- **技術**: React 19、TypeScript(strict)、Vite、React Router、TanStack Query(サーバーの状態)。画面の部品のライブラリは、入れていない(CSS は 1 ファイル。OS の設定に従って、ライト・ダークが切り替わる)。
- **配り方**: 開発は、Vite の開発サーバー(HMR つき)を、nginx(`https://localhost:18443`)の後ろに置く。本番は、`frontend/dockerfile` の `Production` が作る、静的ファイルの nginx。同じホストの `/` に置き、**`/api/` と `/auth/` を backend と auth へ、それ以外を frontend へ**、Traefik で振り分ける(パスは書き換えない)。
- **ビルド・テストは、コンテナの中で**(ホストに Node は要らない): `task frontend:test`(型の検査とテスト)、`task frontend:build`(本番のイメージ)。

```
frontend/
  dockerfile            Develop(開発サーバー)/ Build / Production(nginx)
  nginx.conf, security-headers.inc   本番の静的配信と、セキュリティのヘッダ
  src/
    api/                HttpClient(トークンの付与、401 での更新と1回だけの再試行)、ApiError、型、API の呼び出し
    auth/               AuthSession(ログイン・ログアウト・更新。React に依存しない)、TokenStore、AuthProvider、RequireAuth
    pages/              ログイン・サインアップ(AuthForm)、プロジェクトの一覧と1つの画面
    components/         Layout(ヘッダ)、ConfirmDialog、ErrorMessage
    lib/                プロジェクト名の検査など、純粋な関数
    test/               偽のサーバー(fetch の代わり)、描画の補助
```

## ログインの仕組み

- **access token は、メモリだけに置く**(15 分)。**refresh token は、localStorage に置く**(90 日。`launchs.refreshToken`)。auth が返すのは、JSON のトークンだけで、Cookie には対応していないため。
- 起動時に、保存してある refresh token で、access token を取り、`/auth/me` でユーザーを確かめる。
- API が 401 を返したら、**access token を更新して、1回だけ、やり直す**。同時に何件が 401 になっても、更新は**1回**にまとめる。更新で refresh token が無効(401)と分かったら、ログアウトの状態にする。接続の失敗など、一時的なエラーのときは、ログアウトにしない(refresh token を残す)。
- ログアウトは、先にこの端末を、ログアウトにしてから、サーバーの refresh token を無効にする(サーバーが失敗しても、この端末は、ログアウトのまま)。他のタブでログアウトされたら(`storage` イベント)、このタブも、ログアウトにする。
- ログイン後に戻る場所は、サイト内のパス(`/` で始まり、`//` で始まらないもの)だけを受け付ける(外部の URL へ飛ばされない)。

### XSS への備え
refresh token が localStorage にあるので、ページ内でスクリプトが動かせると、盗まれる。そのため:
- **本番の静的配信に、厳しい CSP を付ける**(`script-src 'self'`、`style-src 'self'` など。インラインのスクリプトとスタイルを禁止。Vite のビルドは、外部ファイルだけを作る)。`X-Frame-Options: DENY`、`nosniff`、`Referrer-Policy: no-referrer` も付ける。本番のイメージで、CSP の違反が 0 件であることを、実ブラウザで確かめた。
- `dangerouslySetInnerHTML` を使わない。外部のスクリプト、フォント、画像を、読み込まない。ユーザーが入力した文字は、React が、エスケープして描画する。
- 将来、auth が HttpOnly Cookie に対応したら、refresh token を、そちらに移す。

## 画面(最初の PR)

| パス | 内容 |
|---|---|
| `/login`、`/signup` | ログイン、アカウント作成(招待制のための、招待コードの欄つき)。サーバーのメッセージと、429 のときは待つ秒数を出す |
| `/projects` | 一覧(「もっと見る」で cursor の続き)、作成(名前を、送る前に検査)。ヘッダに、ユーザー名と、プロジェクトの使用量(`GET /api/v1/quota`)。削除中のものがあれば、3 秒ごとに状態を取り直す |
| `/projects/:projectId` | 名前の変更、削除(確認のダイアログ。削除中は、進み具合を見せ、終わると、一覧へ戻る案内)。`delete_failed` のときは、もう一度、削除できる |

## まだ無いもの(次の PR 以降)

- **deploy の一覧**: 仕様(`GET /api/v1/projects/:projectId/deploys`)にはあるが、**backend に、まだ実装されていない**(build の履歴の一覧も同じ)。backend に足してから、画面を作る。
- deploy の詳細・スケール・設定・マウント、ログ(`logPods`、`since` でのポーリング)、service、ingress、volume、クォータの画面、管理者の画面(ユーザー、招待、上限の変更)。

## 確認した範囲

- **単体・画面のテスト 59 件**(偽のサーバーで、トークンの更新の競合、エラーの表示、検査、cursor、確認のダイアログなど)。
- **実ブラウザ(Chromium)で、本物の backend・auth・worker につないで、14 の手順**: 未ログインのガード、ログインの失敗、サインアップ(弱いパスワードの拒否と成功)、プロジェクトの作成・重複(409)・名前の変更・削除(namespace まで)、リロードでの復帰、**access token を壊しての、自動更新**、ログアウト、ログインし直し、モバイル幅(390px)で横にはみ出さないこと、ダークモード。
- 実ブラウザの確認の手順は、リポジトリには入れていない(Playwright の Docker イメージが要る)。必要なら、`frontend/e2e/` として足す。

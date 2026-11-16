# フロントエンド(`frontend/`)

React + Vite + TypeScript の SPA。バックエンドの API(`/api/v1/...`)と、認証(`/auth/...`)を、同じオリジンから呼ぶ(CORS は要らない)。

## 決まっていること

- **技術**: React 19、TypeScript(strict)、Vite、React Router、TanStack Query(サーバーの状態)。
- **デザイン**: **Tailwind CSS v4 + shadcn/ui**。shadcn/ui の標準(zinc の無彩色、小さめの文字、細い枠、黒(ダークでは白)のボタン)に寄せた、シンプルな見た目。アイコンは lucide。ライトとダークは、OS の設定に従う。
- **レイアウト**: **パソコンの画面が前提**。左に、固定のサイドバー(ロゴ、ナビゲーション、プロジェクトの使用量、ユーザーとログアウト)、右に、画面の内容を、幅いっぱい(最大 72rem)に広げる。幅が 768px 未満のときは、サイドバーを隠して、ロゴだけのバーにする(最低限の表示で、モバイルの最適化は、していない)。
  - **shadcn/ui の部品は、`src/components/ui/` に、ソースとして置く**(button、input、label、card、badge、alert、skeleton、spinner)。見た目を変えるときは、そのファイルを直す。
  - **色は、`src/styles.css` の変数**(`--primary`、`--card`、`--border` など。shadcn/ui と同じ名前)で決まる。ライトとダークの値が、1 か所にある。部品は、色を直接書かず、この名前を使う(`bg-primary`、`text-muted-foreground`)ので、配色を変えるときは、変数だけを直す。
  - **外のフォントは読み込まない**(CSP)。Google Sans が入っていれば使い、無ければ、Roboto や OS の標準のフォント。
  - **確認のダイアログは、標準の `<dialog>`**(`ConfirmDialog`)。shadcn/ui の標準(Radix の Dialog)は、画面のスクロールを止めるために、`<style>` を差し込むことがあり、厳しい CSP(`style-src 'self'`)に違反するおそれがあるため、使っていない。Esc で閉じる、フォーカスが閉じ込められる、背面は操作できない、は、標準の機能で満たしている。
  - 部品を足すときは、shadcn/ui の部品のうち、インラインのスタイルや、`<style>` を足さないものを選ぶ(足すなら、本番のイメージで、CSP の違反が 0 件のままか、確かめる)。
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
    components/         Layout、Sidebar、PageHeader(パンくずとタイトル)、Brand、ConfirmDialog、ErrorMessage。ui/ は shadcn/ui の部品(button、input、label、card、badge、alert、table、progress、skeleton、spinner)
    lib/                プロジェクト名の検査、状態の言葉と色など、純粋な関数。utils.ts は、クラス名を組み合わせる cn
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
| `/projects` | 表(名前、状態、プロジェクト ID。行の全体を押せる)、「もっと見る」で cursor の続き、読み込み中は仮の帯、作成(名前を、送る前に検査)。サイドバーに、プロジェクトの使用量(`GET /api/v1/quota`。8 割で黄、上限で赤)。削除中のものがあれば、3 秒ごとに状態を取り直す |
| `/projects/:projectId` | プロジェクトの画面。名前の変更、削除に加えて、**deploy の一覧**(新しい順、cursor、状態が変わっていく間は 3 秒ごとに取り直す)と、**deploy の作成**(イメージ + スペック small/medium/large)。作成すると、その deploy の画面へ進む |
| `/deploys/:deployId` | **deploy の画面**。状態(起動中・反映中などは 2 秒ごとに取り直す)、稼働中の Pod、**ログ**(`logPods` から Pod を選び、`since` で続きを取り続ける。稼働中の Pod は 2 秒ごとに新しい行を待つ。画面に持つのは最大 50 万文字)、**スケール**(0〜5。反映中・起動中・削除中は押せない)、**削除**(確認のダイアログ。削除が終わると、404 の案内が出る)。config・service・volume の編集は、これから |
| `/usage` | **使用量**。自分の上限と使用量(プロジェクト、deploy の合計、volume の数と容量、スペックごとの deploy、volume 1つの上限、replicas の最大)。棒は、値と最大が、読み上げに伝わる |
| `/projects/:projectId` | パンくず、名前の変更、削除(確認のダイアログ。削除中は、進み具合を見せ、終わると、一覧へ戻る案内)、右に詳細のカード。`delete_failed` のときは、もう一度、削除できる |

## 人間であることの確認(Cloudflare Turnstile)

ログインとサインアップに、Cloudflare Turnstile を付けられる。**`.env` に `TURNSTILE_SITE_KEY` と `TURNSTILE_SECRET_KEY` を書いたときだけ有効**(設計と、サーバー側は、[docs/auth.md](auth.md) §16)。

- ログイン画面を開くと、`GET /auth/config` で、有効かと、サイトキーを知る。有効なら、Cloudflare のスクリプトを読み込んで、ウィジェットを描く(`components/TurnstileWidget.tsx`、`auth/turnstile.ts`)。サイトキーは、ビルドに焼き込まない。
- トークンができるまで、送信ボタンは押せない。送信に失敗したら、トークン(1回しか使えない)を捨てて、ウィジェットを作り直す。期限切れ・エラーのときも、ボタンは、また押せなくなる。
- **本番**: 静的配信の nginx に、環境変数 `CSP_TURNSTILE=https://challenges.cloudflare.com` を渡す(渡さないと、CSP が、Cloudflare のスクリプトを遮断する)。渡したときだけ、`script-src`、`connect-src`、`frame-src` が、このオリジンを許す。開発の Vite の開発サーバーには、CSP が無いので、不要。

## まだ無いもの(次の PR 以降)

- **deploy の一覧**: 仕様(`GET /api/v1/projects/:projectId/deploys`)にはあるが、**backend に、まだ実装されていない**(build の履歴の一覧も同じ)。backend に足してから、画面を作る。
- deploy の詳細・スケール・設定・マウント、ログ(`logPods`、`since` でのポーリング)、service、ingress、volume、管理者の画面(ユーザー、招待、上限の変更)。画面が増えたら、サイドバーの `NAV_ITEMS`(`components/Sidebar.tsx`)に足す。

## 確認した範囲

- **単体・画面のテスト 80 件**(Turnstile を含む)(偽のサーバーで、トークンの更新の競合、エラーの表示、検査、cursor、確認のダイアログなど)。
- **実ブラウザ(Chromium)で、本物の backend・auth・worker につないで、14 の手順**: 未ログインのガード、ログインの失敗、サインアップ(弱いパスワードの拒否と成功)、プロジェクトの作成・重複(409)・名前の変更・削除(namespace まで)、リロードでの復帰、**access token を壊しての、自動更新**、ログアウト、ログインし直し、モバイル幅(390px)で横にはみ出さないこと、ダークモード、確認のダイアログ。
- 実ブラウザの確認の手順は、リポジトリには入れていない(Playwright の Docker イメージが要る)。必要なら、`frontend/e2e/` として足す。

# フロントエンド(`frontend/`)

Next.js + React + TypeScript。画面は v0 で作ったものを土台にしている(以前の Vite + shadcn/ui 版は、置き換えた)。

## 動かし方
- 開発: `docker compose up -d`(frontend のコンテナが `next dev` をポート 3000 で動かし、nginx が `/` を向ける)。ブラウザは `https://<HTTPS_BIND_ADDRESS>:<HTTPS_HOST_PORT>/`(既定は `https://127.0.0.1:18443/`)。
- API の相手: 環境変数 `NEXT_PUBLIC_LAUNCHS_API_MODE=live` のとき、同じホストの `/api/v1/...` と `/auth/...`(nginx が backend と auth に振り分ける)。**未指定だと、見せかけのデータ(`lib/mock-api.ts`)で動く**。compose と dockerfile は、live にしてある。
- 別ホスト名でつなぐとき: Next.js の開発サーバーは、知らないホスト名からの開発用の通信を断る。`.env` の `HTTPS_BIND_ADDRESS` を、`DEV_ALLOWED_ORIGINS` に自動で渡している(`next.config.mjs` の `allowedDevOrigins`)。
- 型の検査: `task frontend:test`(`tsc --noEmit`)。
- 本番のイメージ: `task frontend:build`(`output: "standalone"` を、Node で動かす。非 root、ポート 3000)。`/api` と `/auth` は、Traefik が backend と auth に振り分ける。

## 構成
- `app/` ページとレイアウト、`components/` 画面の部品(`views/` が、概要・プロジェクト・deploy・volume・ingress・ログ・アカウント)、`lib/api.ts` が API の呼び出し(access token はメモリ、refresh token は保持して 401 で更新)、`lib/mock-api.ts` が見せかけの API。
- ログインとサインアップは、人間であることの確認(Turnstile)に対応(`GET /auth/config` のサイトキー)。
- 画面は、開いている間、5 秒ごとに自動で更新する(右上の一時停止と更新のボタン)。

## ConfigMap の画面(docs/configmap.md の段階 3)
- 上のバーの「ConfigMap」タブ(`components/views/configmaps-view.tsx`): 一覧と編集。ファイルは、アップロード(複数、ドラッグアンドドロップ。UTF-8 で NUL を含まなければテキスト、それ以外はバイナリ=base64)か、「+ 書いて追加」で足す。テキストは、その場で編集でき、バイナリは、差し替えとダウンロードだけ。保存すると、新しい版ができる(同じ中身なら増えない)。版の一覧から、古い版を読み取り専用で見て、「この版の内容で、下書きを置き換える」で戻せる。使っているデプロイの一覧が出て、使用中は削除できない。
- deploy のパネルの「ConfigMap」タブ(`components/panels/configmap-mounts-tab.tsx`): ConfigMap とファイルを選び、マウント先のパスと権限(mode)を入れる。「全ファイルを、ディレクトリに追加」もある。ConfigMap を編集しても、deploy には届かない(版を固定)。「新しい版あり」が出たら、「最新にする」→「保存」→「反映」。未反映の変更は、上のバーの「反映待ち」にも出る。
- 上限の検査(保存する大きさの合計 1 MiB、50 ファイル、ファイル名、パス、mode)は、`lib/configmap-files.ts` に、バックエンドと同じ規則で持つ(送る前に止める)。ConfigMap の中身は大きい(最大 1 MiB)ので、自動更新の対象にしていない(保存したときと、開いたときだけ取る)。
- 見せかけのデータ(`lib/mock-api.ts`)にも、同じ形を足してある。

## 既知の不足(以前の版にあって、まだ無いもの)
- 自動テスト(vitest)が無い。
- 厳しい CSP と、Turnstile 用の CSP の切り替えが無い(`next.config.mjs` には、基本のヘッダだけ)。
- 画面の細かい仕様(入力の検査など)は、以前の版より少ない。

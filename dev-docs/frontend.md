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

## ConfigMap の画面(dev-docs/configmap.md の段階 3)
- 上のバーの「ConfigMap」タブ(`components/views/configmaps-view.tsx`): 一覧と編集。ファイルは、アップロード(複数、ドラッグアンドドロップ。UTF-8 で NUL を含まなければテキスト、それ以外はバイナリ=base64)か、「+ 書いて追加」で足す。テキストは、その場で編集でき、バイナリは、差し替えとダウンロードだけ。保存すると、新しい版ができる(同じ中身なら増えない)。版の一覧から、古い版を読み取り専用で見て、「この版の内容で、下書きを置き換える」で戻せる。使っているデプロイの一覧が出て、使用中は削除できない。
- deploy のパネルの「ConfigMap」タブ(`components/panels/configmap-mounts-tab.tsx`): ConfigMap とファイルを選び、マウント先のパスと権限(mode)を入れる。「全ファイルを、ディレクトリに追加」もある。ConfigMap を編集しても、deploy には届かない(版を固定)。「新しい版あり」が出たら、「最新にする」→「保存」→「反映」。未反映の変更は、上のバーの「反映待ち」にも出る。
- 上限の検査(保存する大きさの合計 1 MiB、50 ファイル、ファイル名、パス、mode)は、`lib/configmap-files.ts` に、バックエンドと同じ規則で持つ(送る前に止める)。ConfigMap の中身は大きい(最大 1 MiB)ので、自動更新の対象にしていない(保存したときと、開いたときだけ取る)。
- 見せかけのデータ(`lib/mock-api.ts`)にも、同じ形を足してある。

## テンプレートから作る画面(dev-docs/templates.md の段階 D)
- 構成図の「+ 追加 → デプロイ」(と、空のプロジェクトの「最初のデプロイ」)で、**入口の 2 択**(「テンプレートから」「イメージから」)が出る(`components/wizard/wizard-launcher.tsx`)。「イメージから」は、今までのウィザード。
- 「テンプレートから」(`components/wizard/template-wizard.tsx`): テンプレートを選ぶ(カード。カテゴリで絞り込み)→ 入力 → 確認 → 作成の進み具合 → 結果。
  - 入力欄は、変数の型で変わる(文字列、数、選択、スイッチ、password)。password は、「自動で作る」(既定)か、手で入れる(「表示」で見られる)。検査(範囲、形式、改行)は、サーバーと同じ規則で、手元でも行う(`lib/template-form.ts`)。
  - 確認では、作るものの要約と、入力の値(password は伏せ字)を出す。デプロイ・ボリュームの数が、クォータを超えそうなら、警告する(超える場合は、サーバーが、何も作らずに断る)。
  - 「作成する」で、受け付け(`POST .../templates/:id/apply`)を頼み、進み具合(`GET .../template-applies/:applyId`)を、2 秒ごとに取って、部品ごと(待機中 → 作成中 → 完了 / 失敗)に出す。構成図も、一緒に更新する。**閉じても、作成は続く**。途中で失敗したら、止まり、失敗した部品と理由が出る(作れたものは、残っている)。
  - 完了したら、公開 URL(ingress のホスト名)と、**全ての変数の値**(自動で作った password を、表示・コピーできる)を出す。
  - 受け付けが断られたとき(入力の不正、クォータ)は、何も作られていないので、確認の画面に留まって、理由を出す。
- 見せかけのデータ(`lib/mock-api.ts`)にも、3 つのテンプレートと、進み具合を足してある(1 ステップ 1.2 秒で進み、終わると、構成図に、部品が現れる)。

## 既知の不足(以前の版にあって、まだ無いもの)
- 自動テスト(vitest)が無い。
- 厳しい CSP と、Turnstile 用の CSP の切り替えが無い(`next.config.mjs` には、基本のヘッダだけ)。
- 画面の細かい仕様(入力の検査など)は、以前の版より少ない。

# アクセスログのエクスポート(設計メモ)

ingress のアクセスログ(`ingress_access_logs`)は、保存が **24 時間、ingress ごとに 10000 行まで**(10000 は、この機能に合わせて、5000 から増やした既定値)([ingress-plugins.md](ingress-plugins.md))。これでは足りない人のために、ログを**外に書き出す**仕組みを作る。**状態: 設計中(未実装)。**

## 決まったこと

- **送り先は 2 種類**: webhook と、Google Drive。(S3 互換のストレージは、要望が出てから。土台を共通にして、あとから足せるようにする)
- **対象は、アクセスログの 1 行ずつ**(集計ではない)。コンテナのログ(`logcollector`)は、対象にしない。
- **送り先は、プロジェクトに属する**。そのプロジェクトの、全ての ingress のログを、まとめて送る(行に、どの ingress のものかが入る)。
- **Google Drive は、1 日ごとにファイルを分ける。プロジェクトごとにフォルダを切る**。
- **専用のコンテナ `exporter/`** を作る。**`watcher` と同じ仕組み**(同時に 1 つだけ動く。リーダー選出は PostgreSQL の advisory lock。鍵は `7100004`。watcher=7100001、logcollector=7100002、accessstats=7100003)。

## 全体の流れ

```
accessstats ──保存──▶ ingress_access_logs ──読む──▶ exporter ──▶ webhook(利用者の受け口)
                                                          └──▶ Google Drive(利用者の Drive)
backend ── 送り先の登録・確認・OAuth ──▶ export_destinations
```

- 書くのは、backend(送り先の設定)と、exporter(送り済みの位置、状態)。ログの表は、今までどおり、accessstats だけが書く。
- exporter は、DB と、外向きの HTTPS だけを使う(k8s は、使わない)。
- **配送の保証は、「少なくとも 1 回」**。送り済みの位置(カーソル)は、**送るのに成功してから**進める。落ちたときは、同じ行を、もう一度送る(受け取る側が、行の `id` で重複を除く)。

## データ(新しい表。マイグレーションは up のみ)

**`export_destinations`**(送り先 1 つ)
- `id`(`export-<uuid>`)、`project_id`、`user_id`、`kind`(`webhook` / `google_drive`)、`name`、`enabled`
- `config`(jsonb): webhook は `url` と `intervalSeconds`(送る間隔。既定 30)。Google Drive は `folderId`、`folderName`、`accountEmail`
- `credential`(bytea): **暗号化した秘密**。webhook は署名の秘密、Google Drive は合鍵(リフレッシュトークン)
- `status`: `active`(動作中)/ `failing`(失敗中。再試行する)/ `reauth_required`(Google の再接続が必要)/ `disabled`
- `last_cursor`(bigint): 送り済みの、`ingress_access_logs.id` の最後。**新しい送り先は、作った時点の最大の `id` から始める**(過去のログは送らない。決め直せる)
- `last_success_at`、`last_error`(利用者に見せる短い理由)、`failure_count`、`next_attempt_at`、`created_at`
- 1 つのプロジェクトに、**5 つまで**。

**`export_drive_files`**(Google Drive の、1 日分のファイル)
- `destination_id`、`ingress_id`、`day`(日付)、`part`(大きすぎたときの分割)、`file_id`、`bytes`、`rows`

## 秘密の扱い

- **暗号化**: AES-256-GCM。鍵は、環境変数 `EXPORT_CREDENTIAL_KEY`(32 バイトを base64 で。本番の Secret に入れる)。**空なら、エクスポート機能は無効**(API は、使えない旨のエラー)。暗号化・復号は、共有のパッケージ(`shared/credential`)に置き、backend と exporter が使う。
- webhook の署名の秘密は、**作ったときに 1 回だけ**画面に出す(あとで見せない。作り直せる)。
- ログ(`logger`)に、URL のクエリ、秘密、トークンを**出さない**。画面に出す URL は、ホストとパスの先頭だけにして、クエリは伏せる。
- 送り先を消したら、Google の合鍵は、**取り消しを依頼する**(`https://oauth2.googleapis.com/revoke`)。失敗しても、削除は進める。

## webhook

- **リクエスト**: `POST <url>`、`Content-Type: application/json`、タイムアウト 10 秒、リダイレクトは辿らない。
- **ヘッダー**:
  - `X-Launchs-Timestamp`: 送った時刻(Unix 秒)。
  - `X-Launchs-Signature`: `sha256=` + `HMAC-SHA256(秘密, タイムスタンプ + "." + 本文)` の 16 進数。受け取る側は、時刻のずれ(5 分)も確かめる。
  - `X-Launchs-Delivery`: `<送り先 ID>-<最後の行の id>`(重複の判定に使える)。
- **本文**: `{"projectId": "...", "destinationId": "...", "logs": [{"id": 123, "ingressId": "...", "host": "...", "time": "2026-11-26T10:42:04.123Z", "method": "GET", "url": "/a?b=1", "status": 200, "durationMs": 1.5, "clientIp": "203.0.113.9", "bytes": 234}]}`。1 回に、**最大 1000 行・1 MB**。行は `id` の昇順。
- **成功は 2xx**。それ以外と、タイムアウトは、失敗。失敗したらカーソルは進めず、**指数的に間隔を延ばして再試行**する(選んだ間隔から始めて、倍々に延ばし、最大 1 時間)。
- **頻度(まとめ送り)**: 送り先ごとに `intervalSeconds`(`10` / `30` / `60` / `300` 秒から選ぶ。**既定 30 秒**)。間隔が来たとき、**新しい行があれば**、まとめて送る(1 件ずつは、送らない)。行が 1000 を超えるときは、続けて送る。ログの元(プラグインの 10 秒ごとの出力)が、そもそも 10 秒単位なので、これより短くはしない。
- **SSRF の対策(必須)**: 利用者が指定した URL に、こちらから接続するため。
  - スキームは `https` だけ(開発用に、環境変数で `http` を許す)。
  - **名前解決のあとの IP を検査**し、その IP に直接つなぐ(DNS の再解決で、すり替えられないように)。ループバック、プライベート(RFC 1918)、リンクローカル(クラウドのメタデータ 169.254.0.0/16 を含む)、CGNAT、クラスター内部(Pod と Service の範囲)、`localhost`、IPv6 のこれらに当たるものは、**登録のときも、送るときも拒否**。
  - プロキシの環境変数は、使わない。
- **テスト送信**: 登録のときに、`{"test": true}` を送って、2xx を確かめられる(画面のボタン)。

## Google Drive

- **権限(scope)は `https://www.googleapis.com/auth/drive.file` だけ**(+ 表示用に `openid email`)。**launchs が作ったファイルとフォルダだけ**に、触れる。利用者の、ほかのファイルは、見えない。合鍵が漏れても、被害は、launchs が作ったログに限られる。
- **接続の流れ(OAuth 2.0、認可コード)**:
  1. 画面の「Google Drive に接続」→ `POST /projects/:projectId/export-destinations/google-drive/authorize` が、Google の同意の画面の URL を返す。`state` は、利用者・プロジェクト・有効期限(10 分)・使い捨ての値を、署名したもの。
  2. 利用者が同意すると、Google が `GET /api/v1/export/google/callback?code=…&state=…` に戻す。**この呼び出しは、Authorization ヘッダーを持たない**ので、`state` の署名で、利用者とプロジェクトを確かめる(1 回だけ有効)。
  3. backend が、`code` を、合鍵に交換し、**暗号化して保存**。フォルダを作る。画面に戻す(`/?export=connected`)。
- **フォルダの構成**(利用者の「マイドライブ」の中):
  ```
  launchs-access-logs/
    <プロジェクト名>/
      <ingress のホスト名>_2026-11-26.ndjson
  ```
  フォルダは、接続のときに作り、ID を保存する(名前を後から変えても、ID で辿る)。利用者がフォルダを消した(ゴミ箱に入れた)ら、次の書き込みのときに、**自動で作り直す**。
- **ファイル**: 1 日(**日本時間 `shared/jst` の 0 時区切り**。環境変数で変えられる)・ingress ごとに 1 ファイル。形式は **NDJSON**(1 行に、ログ 1 件。webhook の `logs` の 1 要素と同じ形)。
- **追記ができない**ため、書き込みは「ダウンロード → 末尾に足す → 全体を、上書きでアップロード」(`files.get` と `files.update`)。**新しい行があるときだけ**行う(無ければ、ファイルを更新しない)。
- **書き込みの時刻は、送り先ごとに、1 時間の中で散らす**(全部の送り先が、同じ時刻に、Drive の API を叩かないように)。Drive の書き込みの時刻は、1 時間を **12 の枠**(`:00`、`:05`、…、`:55`)に分け、送り先ごとに、**枠を 1 つ割り当てる**(送り先 ID のハッシュ ÷ 12 の余り。作成時刻より、均等に散る)。**自分の枠の時刻に、1 時間分をまとめて書く**(例: 枠が `:25` なら、毎時 25 分)。
- **前倒し**: 自分の枠でなくても、**たまった行が 6000 行(上限 10000 行の 6 割)を超えたら、すぐに**書く(10 秒ごとの見回りで気づく)。
- どちらも、**成功してから、カーソルを進める**。1 つのファイルが **50 MB** を超えたら、`_part2` に切り替える。
- 環境変数: `EXPORT_CHECK_INTERVAL`(見回りの間隔。既定 10s)、`EXPORT_DRIVE_INTERVAL`(Drive の書き込みの周期。既定 1h。12 の枠は、この周期を 12 等分する)、`EXPORT_DRIVE_FLUSH_ROWS`(既定 6000)。
- **再認可(再接続)**: 合鍵が効かなくなったとき(`invalid_grant`。利用者が許可を取り消した、長く使わなかった、など)は、`status` を `reauth_required` にして、**書き込みを止める**。画面に「再接続が必要です」と出し、ボタンで、同意の流れをもう一度行う(同じ送り先に、新しい合鍵を入れる)。
- **Google 側の準備(運営の作業)**: Google Cloud でプロジェクトを作り、OAuth クライアント(Web)を作る。リダイレクト URI は `https://<LAUNCHS_HOST>/api/v1/export/google/callback`。同意の画面は、**「本番」に公開**する(`drive.file` は、審査の軽い権限。**「テスト」のままだと、合鍵が 7 日で切れる**)。クライアント ID と秘密は、環境変数 `GOOGLE_OAUTH_CLIENT_ID`、`GOOGLE_OAUTH_CLIENT_SECRET`(本番の Secret)。**両方が空なら、Drive は無効**(webhook は使える)。

## 取りこぼしの限界(正直に)

- exporter が送る前に、**ログの保存の上限(ingress ごとに 10000 行、24 時間)で消える**と、そのログは送れない。アクセスが多い ingress や、長く止まっていたとき(失敗が続いて、24 時間を過ぎたとき)に、起こる。
- Drive は 1 時間ごとなので、**1 つの ingress が 1 時間に 10000 行(約 2.8 件/秒)を超えるとき**は、上の「6000 行で前倒し」(10 秒ごとの見回り)で拾える。ログの保存の上限(10000 行)を、見回りの間(10 秒)に超えるほどの量(1000 件/秒)でなければ、取りこぼさない。
- **そもそも、記録されない分がある**: プラグインは、10 秒ごとの出力に、1 リクエストごとの記録を **200 件まで**しか入れない(約 20 件/秒。超えた分は、集計の件数にだけ入る。[ingress-plugins.md](ingress-plugins.md))。エクスポートは、この**記録されたログ**を送るので、20 件/秒を超えるアクセスは、全件は、送れない。
- 対策: (1) 送る間隔を短くする(webhook は 10〜300 秒から選ぶ。Drive は、行数での前倒し)。(2) `ACCESS_LOG_MAX_ROWS` と `ACCESS_LOG_RETENTION` を、増やせる。(3) 画面で、「失敗が続くと、ログが失われる」ことを、警告する。(4) **段階 2 の候補**: accessstats の削除が、有効な送り先の、送り済みの位置より新しい行を、消さない(上限は、3 倍までなど、守る)。
- `id` は、書き込む順に増えるが、accessstats(1 つだけ書く)が、1 回の取り込みを 1 つのトランザクションで書くので、**カーソルが、後から確定した行を、飛ばすことは、ほぼ無い**(飛ばしたとしても、24 時間の中の話)。

## API(案。すべて、プロジェクトの持ち主だけ。他人のものは 404)

- `GET /projects/:projectId/export-destinations` — 一覧(状態、最後の成功、最後のエラー)。
- `POST /projects/:projectId/export-destinations` — webhook を作る(`{"kind":"webhook","name":"…","url":"https://…"}`)。レスポンスに、署名の秘密を**1 回だけ**入れる。
- `PATCH /projects/:projectId/export-destinations/:id` — 名前、有効・無効、webhook の URL。
- `DELETE /projects/:projectId/export-destinations/:id` — 削除(Drive は、合鍵の取り消しを依頼する。Drive の中の、書き出したファイルは、残す)。
- `POST /projects/:projectId/export-destinations/:id/test` — テスト送信(webhook)。Drive は、`launchs-connection-test.txt` を作って確かめる。
- `POST /projects/:projectId/export-destinations/google-drive/authorize` — 同意の画面の URL を返す(再接続は、`{"destinationId": "…"}` を付ける)。
- `GET /export/google/callback` — Google からの戻り先(認証ヘッダー無し。`state` で確かめる)。
- 仕様の正本は、実装するときに `endpoints/export_destinations.md` を作る。

## 画面(案)

プロジェクトの上のバーに、**「エクスポート」タブ**を足す。
- 送り先の一覧(種類、名前、状態のバッジ: 動作中・失敗中・再接続が必要・停止中。最後の成功の日時。最後のエラー)。
- 「webhook を追加」(名前、URL。作ると、署名の秘密を、1 回だけ表示して、コピーさせる)。
- 「Google Drive に接続」(同意の画面へ。戻ると、一覧に出る)。「再接続」ボタン(状態が `reauth_required` のとき)。
- 各行: テスト送信、有効・無効の切り替え、削除。
- 作るときの注意書き: **ログには、URL(クエリを含む)と、クライアントの IP が入る**。送り先を、信頼できる場所にすること。

## コンテナ `exporter/`

- 構成は、`accessstats/` と同じ(`dockerfile`、`src/main.go`、`src/config`、`src/deliver`(webhook と Drive の送り手)、`.air.toml`)。
- 動き: **10 秒ごと**に見回り、送り先ごとの「次に動く時刻」(`next_attempt_at`)を過ぎたものだけ、動く。webhook は、選んだ間隔(既定 30 秒)ごと。Drive は、自分の枠の時刻のときと、行数の前倒しのとき。、`enabled` で、`next_attempt_at` を過ぎた送り先を数え、**送り先ごとに順番に**(送り先どうしは、最大 4 つまで、並行して)送る。1 回の取り込みは、`id > last_cursor` かつ、そのプロジェクトの ingress の行を、`id` の昇順で 1000 行まで。
- 設定(環境変数): `DATABASE_URL`、`EXPORT_CREDENTIAL_KEY`、`GOOGLE_OAUTH_CLIENT_ID` / `GOOGLE_OAUTH_CLIENT_SECRET`、`EXPORT_CHECK_INTERVAL`、`EXPORT_DRIVE_INTERVAL`、`EXPORT_DRIVE_FLUSH_ROWS`(上の Google Drive の節)、`EXPORT_ALLOW_HTTP`(開発用。既定 false)、`LOG_LEVEL`。
- 本番(`deploy/`): `exporter.yaml`(Deployment。watcher と同じ形)と、`release.yml` のイメージ一覧への追加。Secret に `EXPORT_CREDENTIAL_KEY`、`GOOGLE_OAUTH_*` を足す。**本番への適用は、別に、許可を取ってから行う**。

## 作る順番

1. **土台と webhook**: 表、`shared/credential`(暗号化)、SSRF を防ぐ HTTP クライアント、backend の API、`exporter/`(webhook)、画面の「エクスポート」タブ。
2. **Google Drive**: OAuth(authorize、callback)、フォルダと日ごとのファイル、再接続。
3. (要望が出てから)S3 互換、取りこぼしを減らすための、accessstats の削除の調整。

## 試験の方針

- webhook: 偽の受け口(`httptest`)で、署名、再試行、カーソル、SSRF の拒否(プライベート IP、リダイレクト、IP のすり替え)を確かめる。
- Google Drive: 偽の Google の API(トークンの交換、`files` の作成・取得・更新)を `httptest` で作り、URL を環境変数で差し替えて確かめる(フォルダの作り直し、`invalid_grant` で `reauth_required` になること、分割)。
- 暗号化: 往復、鍵が違うと復号できないこと、改ざんの検出。
- API: 他人の送り先が 404、上限(5 つ)、秘密が 2 回目以降は出ないこと。

## まだ決めていないこと(確認したい)

1. **日付の区切り**は、日本時間でよいか(利用者が、ほぼ日本の人なら、そのままでよい。プロジェクトごとに変えられるようにするかは、後で)。
2. **新しい送り先は、「今から」送る**でよいか(過去 24 時間分も送る選択肢を、付けるか)。
3. **ログの保存を、24 時間から延ばす**か(例: 7 日)。延ばすと、DB の容量が増える(1 行 約 300 バイトとして、10000 行 × ingress 数 = 1 ingress で約 3 MB)。行数の上限を、10000 に増やしたので、24 時間のままでも、まずは足りるかもしれない。
4. **1 つのプロジェクトの送り先の上限**は、5 つでよいか。

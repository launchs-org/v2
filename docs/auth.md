# 認証とクォータの設計

**状態: 承認済み。auth コンテナの土台(§9 の 1)を実装した。** 実装の差分は、末尾の「実装メモ」を見る。 API の仕様は [endpoints/auth.md](endpoints/auth.md)、決定の経緯は [pre-implementation-checklist.md](pre-implementation-checklist.md) の C(認証)と G-4(リソース制限)にある。この文書は、それらで決めていない「中身の設計」を決めるためのもの。**「要確認」と書いた所が、レビューしてほしい点。**

## 1. 目的と、決まっていること

目的: 開発用の仮の認証(`DEV_USER_ID`)を、本物の認証に置き換え、アカウントごとの上限(クォータ)を設ける。公開前の必須条件。

すでに決まっていること(変えない):

- 認証は、別のコンテナ(`auth/`)。signup / login / refresh / me / logout と JWKS を担当する。ユーザーの DB は、アプリと同じ PostgreSQL サーバーの、別のデータベース(`auth`)。
- URL は、認証が `/auth/...`、それ以外が `/api/v1/...`(Traefik がパスで振り分ける)。
- access token: 15分、EdDSA(Ed25519)。失効の管理はしない。backend は、JWKS の公開鍵で、ローカルに検証する。
- refresh token: 90日(固定)、更新しない。`jti` を DB で管理する(ログアウトと BAN で無効)。
- パスワード: argon2id、12文字以上、よくあるパスワードと漏洩済みのパスワードを拒否する。
- ログイン: ユーザー名ごとに5回失敗で15分ロック、IP ごとに 20回/分。サインアップ: IP ごとに 10回/時。存在しない・誤り・ロック中・BAN は、同じ 401。
- サインアップは、公開と招待制を、設定で切り替えられる。
- クォータは、アカウントごと。スペック(small / medium / large)と、クォータの値は、DB のマスターデータ。1 volume の上限は 5GB(確定)。

## 2. 構成

```
 利用者 ──▶ Traefik ──┬─ /auth/...     ─▶ auth コンテナ ─▶ PostgreSQL(auth DB)
                      └─ /api/v1/...   ─▶ backend      ─▶ PostgreSQL(app DB)
                                            │  ▲
                                            │  └─ JWKS を取得(/auth/.well-known/jwks.json、キャッシュ)
                                            └─ access token を、公開鍵で検証 → userId を取り出す
```

- `auth/` は、他のコンテナと同じ形(`auth/dockerfile`、`auth/src/`、中は controller / service / repository / model)。Go モジュールは、同じ `launchs`。共有するのは `shared/` の、設定の読み込み・ロガー・apperror・database だけ。
- マイグレーションは、`migrations/auth/`(up のみ)。compose に `migrate-auth` を足す(`migrate-app` と同じ形)。
- backend と auth は、**DB を共有しない**。backend が知っているのは、トークンの `sub`(userId)と `name`(userName)だけ。

## 3. auth のデータ(auth DB)

| テーブル | 主な列 | 役割 |
|---|---|---|
| `users` | `id`(`userid-` + UUID v4)、`user_name`、`user_name_lower`(UNIQUE)、`password_hash`、`status`(`active` / `banned`)、`role`(`user` / `admin`)、`created_at` | ユーザー。重複の判定は、`user_name_lower` |
| `refresh_tokens` | `jti`(PK)、`user_id`、`issued_at`、`expires_at`、`revoked_at` | refresh token の有効・無効。ログアウトで `revoked_at`、BAN で、そのユーザーの全てに `revoked_at` |
| `login_failures` | `user_name_lower`(PK)、`failed_count`、`locked_until` | ユーザー名ごとの失敗の回数とロック |
| `rate_limit_events` | `kind`(`login` / `signup`)、`ip`、`occurred_at` | IP ごとのレート制限(スライディングウィンドウ) |
| `login_events` | `id`、`user_id`(NULL 可)、`user_name_lower`、`kind`(`signup` / `login`)、`result`(`success` / `failure`)、`reason`、`ip`、`user_agent`、`occurred_at` | ログイン履歴。本人が `GET /auth/login-history` で見られる。レート制限に当たった試行は残さない。既定で180日残す |
| `invitations` | `code_hash`(UNIQUE)、`created_by`、`expires_at`、`used_by`、`used_at` | 招待制のときの招待(コードは、ハッシュで保存) |

- 期限切れの `refresh_tokens`、古い `rate_limit_events` は、定期的に消す(watcher の pruner と同じ形で、auth コンテナの中で動かす)。
- 複数の auth コンテナを同時に動かしても、カウントが壊れないよう、カウントは、DB の行を `UPDATE ... RETURNING` で更新する(メモリには持たない)。

## 4. トークンと署名鍵

- claims は、仕様書のとおり(`iss`、`sub`、`name`、`iat`、`exp`、`jti`、`type`)。**access token に、`role`(`user` / `admin`)を足す**(要確認。管理者の操作を、backend が判定するため。足さない場合は、管理の API を、auth だけに置く)。
- **署名鍵の置き方**: PEM のファイルを、ディレクトリ(`AUTH_KEYS_DIR`)に置く。ファイル名が `kid`(例: `key-2026-11.pem`)。署名に使うのは、`AUTH_ACTIVE_KID` の鍵。JWKS には、ディレクトリの全ての鍵の公開鍵を載せる。
  - 鍵の入れ替え: 新しい鍵を足す → `AUTH_ACTIVE_KID` を変えて再起動 → 15分以上たってから、古い鍵のファイルを消す。仕様書の「古い鍵を15分残す」と合う。
  - 本番は、Kubernetes の Secret をマウントする。開発用は、ディレクトリが空なら、起動時に鍵を作る(`AUTH_KEYS_DIR` が、書き込める所のとき)。
  - DB に秘密鍵を入れない(DB の流出で、トークンを偽造できてしまうため)。
- JWKS のレスポンスは、`Cache-Control: max-age=300`。
- refresh token の `jti` は、DB の行と一致し、`revoked_at` が無く、`expires_at` 前のとき、有効。`type` が `refresh` のトークンは、`/refresh` と `/logout` だけで使える(access token として backend に出しても、`type` で拒否する)。

## 5. パスワードとアカウントの保護

- argon2id のパラメータ: メモリ 64MiB、反復 3、並列 2(OWASP の推奨の範囲)。ハッシュは、パラメータごと文字列に入れる(あとから強くできる。ログインの成功のとき、パラメータが古ければ、ハッシュを作り直す)。
- **よくあるパスワード**: 上位1万件ほどの一覧を、バイナリに埋め込んで(`go:embed`)、照合する。**漏洩済みの照合**(Have I Been Pwned の k-anonymity API)は、外への通信が要るので、環境変数 `AUTH_PWNED_CHECK=true` のときだけ行う(既定は off。要確認)。
- **ユーザーの存在を推測させない**: 存在しないユーザーでも、ダミーのハッシュで argon2id の照合を行い、応答の時間をそろえる。
- **IP の取り方**: backend の前に Traefik があるので、`X-Forwarded-For` を使う。ただし、信頼できるプロキシ(`AUTH_TRUSTED_PROXY_CIDRS`)から来たときだけ。そうでなければ、接続元の IP。
- ロックと、IP の制限の値(5回、15分、20回/分、10回/時)は、環境変数で変える(仕様書の「設定で変えられる」)。
- **入口の守り**:
  - リクエストのボディは 16KiB まで(超えたら 413)。HTTP サーバーに、読み書きと待機のタイムアウトを付ける(遅い接続で、接続を占有されないため)。
  - **argon2id の同時計算数に上限**を付ける(`AUTH_HASH_MAX_CONCURRENCY`、既定 4。1回で 64MiB を使うので、メモリを守る)。空きを 2 秒待っても取れなければ、`Retry-After: 1` つきの 429(失敗には数えない)。
  - IP のレート制限は、上限に達したら、記録を増やさない(大量に送られても、DB の行が増えない)。同じ種類・IP の判定は、アドバイザリロックで順番に行う。
  - トークンを含むレスポンスには、`Cache-Control: no-store` を付ける(JWKS だけ、5分のキャッシュを許す)。

## 6. アカウント管理(管理者)(実装済み。§14)

仕様書の「アカウント管理グループ」にあたる。

- 管理者は、`users.role = 'admin'`。最初の管理者は、auth コンテナのコマンド(`auth admin create <userName>`)で作る(API では作れない)。
- 管理の API(`/auth/admin/...`、`role=admin` の access token が要る。403):
  - ユーザーの一覧・取得、BAN と解除(BAN で、そのユーザーの refresh token を全て無効にする)、ログインのロックの解除
  - 招待の発行・一覧・取り消し
  - サインアップの公開範囲の切り替え(`AUTH_SIGNUP_MODE=open|invite` の環境変数で足りるなら、API は作らない。**要確認**)
- クォータの管理(§8)も、管理者だけ。

## 7. backend の変更

- **認証のミドルウェアを、JWKS の検証に置き換える**。`Authorization: Bearer <accessToken>` を読み、次を確かめる: ヘッダの `kid` の公開鍵で署名が合う、`alg` が `EdDSA`、`iss` が `launchs-org`、`type` が `access`、`exp` が未来。通れば、`sub` を userId として、context に入れる(今の `UserIDFrom` と同じ使い方。**controller 以降は、変えない**)。失敗は、全て 401。
- JWKS は、起動時に取得してキャッシュする。知らない `kid` が来たら、取得し直す(ただし、1分に1回まで。偽の `kid` で、auth を攻撃させないため)。auth に届かないときも、キャッシュの鍵で検証を続ける。
- 設定: `AUTH_JWKS_URL`(例: `http://auth:8080/auth/.well-known/jwks.json`)。**空のときは、起動しない**。ただし、`DEV_USER_ID` が指定されているときだけ、今の仮の認証を使う(開発用。起動のログに、警告を出す)。本番で、`DEV_USER_ID` を指定すると、全員が同じユーザーになるので、**`AUTH_REQUIRE_JWKS=true`(本番)のときは、`DEV_USER_ID` があると、起動しない**。
- **既存のデータ**: `projects.user_id` などは、文字列で、外部キーも無い。開発用のデータ(`user-dev`)は、そのまま残る(本物のユーザーのものにはならない)。移行は、要らない。
- nginx(開発用)と Traefik(本番)に、`/auth/` を auth コンテナへ向けるルールを足す。

## 8. クォータ(実装済み。§13)

### 8.1 置き場所と、見る場所

- **アプリの DB(backend の側)に置く**。deploy などを作るときに、同じトランザクションで確かめるため。
- テーブル: `quota_defaults`(1行。全ユーザーの既定)と、`account_quotas`(`user_id` ごとの上書き。NULL の列は、既定を使う)。値は、データなので、DB を更新すれば変わる。
- 確かめる場所: backend の repository の、作成・変更のトランザクションの中。**そのユーザーの `account_quotas` の行(無ければ作る)を `FOR UPDATE` でロックしてから、使用量を数える**。同時に2つのリクエストが来ても、上限を超えない。
- 上限を超えたときは、**409 `conflict`**(メッセージに、どの項目が、いくつ中いくつか、を入れる)。volume の大きさだけは、仕様書どおり 400(入力の値の問題のため)。(要確認)
- 使用量は、`GET /api/v1/quota`(自分の上限と、使用量)で見られる。

### 8.2 項目と、初期値の案(要確認)

| 項目 | 初期値 | 確かめるとき | 数え方 |
|---|---|---|---|
| プロジェクトの数 | 10 | プロジェクトの作成 | `deleting` 以外 |
| deploy の数 | 20 | deploy の作成 | `deleting` 以外 |
| スペックごとの deploy の数 | small 20 / medium 10 / large 5 | deploy の作成 | スペックごとの、`deleting` 以外の deploy の数 |
| volume の数 | 10 | volume の作成 | 全て |
| volume の合計(MB) | 20480 | volume の作成、拡張の保存 | `size_mb` と、`pending_size_mb` の大きい方の合計(適用前でも、予約として数える) |
| 1 volume の大きさ(MB) | 5120(確定) | volume の作成、拡張の保存 | — |

- ingress は、仕様書どおり、プロジェクトごとの上限なし(クォータの項目にしない)。service は、deploy に1つなので、deploy の数で足りる。
- **スケール(replicas)は、クォータの対象ではない。** 1つの deploy の replicas は、全体で 0〜5(定数 `model.MaxReplicas`。超えたら 400)。CPU・メモリの合計も、replicas の合計も、数えない(スペックの大きさを変えても上限が変わらず、利用者にも分かりやすいため)。減らすときと、`scaled_zero` に戻すときは、確かめない。
- 既存のデータが、あとから下げた上限を超えていても、消さない(新しく作れないだけ)。

### 8.3 管理

- 上限の変更は、管理者だけ(`PUT /api/v1/admin/users/:userId/quota`、`role=admin` の access token)。`quota_defaults` の変更は、マイグレーション(または SQL)で行う。
- volume の上限の引き上げの「申請」の仕組みは、作らない(仕様書の「申請の手段は別途」のまま。管理者が、直接変える)。

## 9. 実装の段階(PR の分け方の案)

1. **auth のコンテナの土台**(新しい `auth/`、マイグレーション、signup / login / refresh / me / logout / JWKS、署名鍵、パスワード、レート制限とロック、テスト、compose と nginx)。
2. **backend の JWKS 検証への置き換え**(ミドルウェア、設定、`DEV_USER_ID` の扱い、テスト)。実機で、auth と backend を通して確かめる。
3. **クォータ**(マイグレーション、`quota_defaults` と `account_quotas`、各作成・変更での確認、`GET /quota`、テスト)。
4. **アカウント管理**(管理者、BAN、招待、クォータの変更の API)。

1 と 2 を、1つにまとめることもできる(まとめると大きくなるので、分ける案)。3 は、1・2 と独立に進められる(今の `DEV_USER_ID` のままでも、動く)。

## 10. レビューしてほしい点(まとめ)

1. access token に `role` を足してよいか(§4)。
2. 漏洩済みパスワードの照合(HIBP)は、既定 off でよいか(§5)。
3. アカウント管理を、最初の PR に含めず、4番目にしてよいか(§6、§9)。
4. サインアップの公開範囲は、環境変数だけでよいか、API で切り替えたいか(§6)。
5. クォータの項目と、初期値(§8.2)。足りない項目、多すぎる項目、値の変更。
6. クォータ超過を、409 にしてよいか(§8.1)。
7. 実装の段階の分け方(§9)。3(クォータ)を、先にしてもよい。

## 11. 実装メモ(PR 1: auth コンテナの土台)

設計から、実装で変えた・足したこと。

- **ログイン履歴を足した**(§3 の `login_events`、`GET /auth/login-history`)。サインアップとログインの成功・失敗を残し、失敗の理由は本人にだけ見せる。存在しないユーザー名への試行も DB には残る(`user_id` が NULL)。
- **共通のミドルウェアを `shared/httpmiddleware` に移した**(アクセスログとエラーハンドラー。backend と auth が同じものを使う)。
- **漏洩済みパスワードの照合(HIBP)は、まだ入れていない**(§5。既定 off の想定だったので、後で足す)。よくあるパスワードは、上位1万件の一覧を埋め込んで照合する(末尾の数字や記号を除いた形も照合)。ほかに、同じ文字の繰り返し、短い並びの繰り返し、連続した文字、ユーザー名を含むものを拒否する。
- **招待制**: 招待コードを検証して使用済みにする部分は入れた(ユーザーの作成と同じトランザクション)。招待を**発行する**手段は、アカウント管理(PR 4)まで無いので、招待制は、それまで、DB に直接招待を入れないと使えない。
- **管理者**(`users.role = 'admin'`)の列と、access token の `role` は入れた。管理者を作る手段と、管理の API は、PR 4。
- **refresh と me**: `/refresh` は、BAN されたユーザーを拒否する(refresh token の有効・無効とは別に、ユーザーの状態を見る)。`/me` は、access token の claims だけから返す(DB を見ない)。
- 設定は、環境変数 `AUTH_*`(`.env.example`)。署名鍵は、`AUTH_KEYS_DIR`(開発用の compose は、`AUTH_KEYS_AUTOGENERATE=true` で、無ければ作る)。

## 12. 実装メモ(PR 2: backend の JWKS 検証)

- **検証は `shared/jwtverify`**(backend 以外のコンテナ、たとえばログ収集も、同じものを使える)。ミドルウェアは `backend/src/middlewares/access_token_middleware.go`。`UserIDFrom` は、そのまま(controller 以降は変えていない)。役割は `UserRoleFrom` で取れる(PR 4 の管理 API で使う)。
- **JWKS の扱い**:
  - 起動時に1度取得する。取得できなくても、起動は止めない(auth が遅れて起動しても、最初のリクエストのときに取得し直す。まだ1度も取れていないときの間隔は 5 秒)。
  - 知らない `kid` が来たら取得し直す。ただし、取得を試みる間隔は 1 分以上(偽の `kid` で、auth を攻撃させない)。
  - キャッシュが 10 分より古ければ、取得し直す。成功したら、鍵を丸ごと置き換える(auth が外した鍵は、使えなくなる)。失敗したら、キャッシュの鍵で、検証を続ける。
  - JWKS のレスポンスは 1MiB まで。使える鍵(OKP / Ed25519 / 署名用)だけを取り出す。
- **失敗は、理由を区別せず、全て 401**(`WWW-Authenticate: Bearer` つき)。
- **設定**(`docs/auth.md` §7 のとおり): `AUTH_JWKS_URL` か `DEV_USER_ID` の、ちょうど一方が必須(両方、どちらも無いときは、起動しない)。`AUTH_REQUIRE_JWKS=true`(本番)のときは、`AUTH_JWKS_URL` が必須で、`DEV_USER_ID` は指定できない。`DEV_USER_ID` で起動したときは、警告をログに出す。
- 開発用の compose は、`AUTH_JWKS_URL=http://auth:8080/auth/.well-known/jwks.json` を指定する(`DEV_USER_ID` は、もう指定しない)。`user-dev` のデータは、DB に残るが、誰のものでもない。

## 13. 実装メモ(PR 3: クォータ)

- **テーブル**: `quota_defaults`(1行)と `account_quotas`(ユーザーごとの上書き。NULL は既定)。**スペックごとの deploy の数**は、既定が `deploy_specs.default_max_deploys`、ユーザーごとの上書きが `account_spec_quotas`(行が無ければ既定)。CPU・メモリの合計では数えない(§8.2)。マイグレーションは `migrations/app/000011_create_quotas.up.sql`。**volume 1つの大きさ**は、これまでどおり環境変数 `VOLUME_MAX_SIZE_MB`(テーブルには入れない)。
- **判定はモデルのメソッド**(`shared/model/quota.go` の `QuotaLimits.Check...`)。repository は、「クォータの行をロックして、使用量を数える → モデルのメソッドで判定 → 保存」だけを行う(`shared/repository/quota_repository.go` の `loadQuota`)。
- **ロックの順番は、どの操作でも、ユーザーのクォータの行が先**(プロジェクトや deploy や volume の行より前)。デッドロックを避けるため。
- **確かめる操作**: プロジェクトの作成、deploy の作成(合計の数と、スペックごとの数)、volume の作成(数と大きさの合計)、volume の拡張の保存(予約が増えるときだけ。同じ値の保存し直しや、破棄は、確かめない)。状態の確認や、入力の確認(400)は、クォータ(409)より先に行う。
- **`GET /api/v1/quota`**: 自分の上限と使用量(ロックしない)。仕様は `docs/endpoints/quota.md`。
- **管理の API(上限の変更)は、まだ無い**(PR 4)。いまは、`account_quotas` を SQL で更新する。
- 認証のコンテナには、クォータの値は無い(アプリの DB にある)。ユーザーの BAN などと、クォータの関係は、PR 4 で決める。

## 14. 実装メモ(PR 4a: アカウント管理)

PR 4 は、小さくするため、4a(auth の管理 API)と 4b(クォータの変更 API。backend)に分けた。ここは 4a。仕様は `docs/endpoints/auth_admin.md`。

- **403 `forbidden`** を、共通のエラー(`shared/apperror`)に足した(管理者用の操作で、管理者でないとき)。
- **管理者かどうかは、毎回、DB の最新の状態で確かめる**(access token の `role` は見ない)。access token は最大15分有効なので、降格・停止された人が、残りの時間で操作できないようにするため。`role` は、backend(クォータの変更 API。4b)が、DB を持たないので、トークンから見る(最大15分の遅れを許す)。
- **最初の管理者は、起動時に作れる(Kubernetes 向け)**: 環境変数 `AUTH_BOOTSTRAP_ADMIN_NAME` と、パスワード(`AUTH_BOOTSTRAP_ADMIN_PASSWORD`、または、Secret をマウントしたファイルを指す `AUTH_BOOTSTRAP_ADMIN_PASSWORD_FILE`)を、セットで指定する。auth の起動時に、**管理者が1人もいないときだけ**作る(停止中の管理者も「いる」と数える。何度起動しても安全。複数の auth が同時に起動しても、1人だけ作られる)。パスワードの検査は通常と同じで、弱い・同じ名前の一般ユーザーがいる(→ `admin promote` を使う)ときは、設定の間違いに気づけるよう、起動を止める。作った後は、Secret から外してよい。ログに、パスワードは出さない。
- **コマンドも残す**(開発用と、管理者を失ったときの復旧用): `task admin:create -- <userName>`(`auth admin create <userName>`)。Kubernetes では、`kubectl exec -i deploy/auth -- <auth のバイナリ> admin create <userName>`(パスワードは標準入力)でも作れる。パスワードは、引数に書くと履歴に残るので、環境変数 `AUTH_ADMIN_PASSWORD` か標準入力で渡す。通常のサインアップと同じ強さの検査を通す。`task admin:promote -- <userName>` で、すでにあるユーザーを管理者にできる(停止中は不可)。
- **BAN**: 状態を `banned` にし、**同じトランザクションで**、そのユーザーの有効な refresh token を全て無効にする。自分自身は停止できない。発行済みの access token は、最大15分、使える(auth が見るもの=管理 API・`/refresh` は、すぐ止まる。backend の API は、access token の期限まで通る)。解除しても、無効にした refresh token は戻らない。
- **招待**: `id`、`note`、`revoked_at` を足した(`migrations/auth/000002`)。コードは、32バイトの乱数で、発行のレスポンスでだけ返す(保存は、SHA-256 のハッシュだけ)。取り消した招待は、サインアップで使えない。
- **サインアップの公開範囲を切り替える API は作らない**(§6 の「要確認」の結論)。環境変数 `AUTH_SIGNUP_MODE` で足りる。切り替えは、再起動になる。
- **管理操作の記録は、ログだけ**(誰が、何を、何に)。DB の監査テーブルは作っていない。必要になったら足す。
- **ユーザーの削除は作らない**(アカウントを消すと、プロジェクトなどの持ち主が、いなくなるため)。使えなくするのは BAN。

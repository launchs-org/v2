[← エンドポイント一覧](README.md)

## 認証関連 (/auth/)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/auth` を省いて書いている(実際の URL は `/auth/signup` など)。

### トークンの仕様
- access token: 有効期限は15分。署名は EdDSA(Ed25519)で、ヘッダの `kid` が公開鍵を指す。バックエンドは [JWKS 取得](#jwks-取得) で得た公開鍵でローカルに検証する。失効の管理はせず、有効期限だけで切れる(ログアウトや BAN の後も、発行済みのものは最大15分有効)
- refresh token: 有効期限は90日(固定)。更新(ローテーション)はしない。認証コンテナだけが発行・検証し、`jti` を DB で管理して有効・無効を判定する(ログアウトや BAN で無効になる)
- どちらも JWT。claims の例(access token には、役割 `role`(`"user"` か `"admin"`)も入る。backend が管理者の操作を判定するため。refresh token には入らない):
```json
{
  "iss": "launchs-org",
  "sub": "userid-80b0b29c-7514-40dd-a0b4-8f22f09329d8",
  "name": "user1",
  "iat": 1794441600,
  "exp": 1794442500,
  "jti": "tokenid-d9cc64ab-216e-460c-b58c-4b5cf36a8295",
  "type": "access"
}
```
- `type` は `"access"` か `"refresh"`。`iat` と `exp` は UNIX 時刻(秒、数値)で、日時の JST ルールは適用しない
- 以下の例では、トークンを `<accessToken>` / `<refreshToken>` と書く

### サインアップ
- Post: /signup
- Header
```json
{"Content-Type": "application/json"}
```
- Body
```json
{"userName": "user1", "password": "Password1234"}
```
- Response (200)
```json
{"token": "<refreshToken>"}
```
- Note: token は refresh token。サインアップ後にそのままログイン状態になる
- Note: 招待制(環境変数 `AUTH_SIGNUP_MODE=invite`)のときは、Body に `"invitationCode": "<招待コード>"` が必要。無い、違う、期限切れ、使用済みの招待は 400。招待は1回だけ使える(ユーザーの作成に失敗したときは、使用済みにならない)
- Note: `userName` は英数字とハイフンのみ、最大64文字。大文字小文字を区別せずに重複を判定し、重複していたら 409 を返す
- Note: `password` は12文字以上。組み合わせ規則は課さず、よくあるパスワードと漏洩済みのパスワードは 400 で拒否する
- Note: 同じ IP からのサインアップは1時間あたり10回まで。超えたら 429(`Retry-After` 付き)を返す
- Note: サインアップの公開範囲(公開 / 招待制)は設定で切り替えられる。招待制のときの挙動は、アカウント管理グループで定義する
- Note: 人間であることの確認(Cloudflare Turnstile)が有効なとき(下の「公開の設定」)は、Body に `"turnstileToken": "<トークン>"` が必要。無い・無効・使用済み・期限切れは 400。確認のサービスに届かないときは 503。確認は、IP のレート制限の後、パスワードの検査より前に行う

### ログイン 
- Post: /login
- Header
```json
{"Content-Type": "application/json"}
```
- Body
```json
{"userName": "user1", "password": "Password1234"}
```
- Response (200)
```json
{"token": "<refreshToken>"}
```
- Note: token は refresh token
- Note: 同じユーザー名で連続5回失敗すると、15分間ロックする。成功したら失敗の回数をリセットする
- Note: 存在しないユーザー、パスワードの誤り、ロック中、BAN 中は、全て同じ 401 を返す(アカウントの存在や状態を推測されないため)
- Note: 同じ IP からのログイン試行は1分あたり20回まで。超えたら 429(`Retry-After` 付き)を返す
- Note: 人間であることの確認(Cloudflare Turnstile)が有効なときは、Body に `"turnstileToken": "<トークン>"` が必要(サインアップと同じ)。確認の失敗(400、503)は、ユーザー名の失敗の回数(ロック)にも、ログイン履歴にも、数えない(他人のユーザー名で、ロックさせられないため)。確認を通った後のパスワードの誤りは、これまでどおり 401 で、数える
- Note: 上の回数と時間は仮の値で、設定で変えられる。ロックと BAN は認証基盤側で管理する(管理機能はアカウント管理グループで定義する)

### 公開の設定
- Get: /config
- Response (200、人間であることの確認が有効なとき)
```json
{"turnstile": {"enabled": true, "siteKey": "0x4AAAAAAA..."}}
```
- Response (200、無効なとき)
```json
{"turnstile": {"enabled": false}}
```
- Note: 認証は不要。ブラウザが、ログイン画面で、Cloudflare Turnstile のウィジェットを出すかと、そのサイトキーを知るための API。サイトキーは公開してよい値で、**シークレットキーは、返さない**
- Note: `enabled` が true のとき、`POST /signup` と `POST /login` に、`turnstileToken` が要る。ウィジェットが作るトークンは、1回しか使えず、約5分で切れる(Cloudflare が確かめる)。失敗したら、ウィジェットを作り直して、新しいトークンで、やり直す
- Note: 設定は、環境変数 `AUTH_TURNSTILE_SITE_KEY` と `AUTH_TURNSTILE_SECRET_KEY`(両方指定すると有効。両方空なら無効。片方だけだと、起動しない)

### アクセストークン (取得)
- Post: /refresh
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer <refreshToken>"
}
```
- Response (200)
```json
{"token": "<accessToken>"}
```
- Note: refresh token は更新されない。有効期限(90日)が切れるまで、何度でも同じ refresh token で access token を取得できる。期限が切れたら再ログインが必要
- Note: refresh token が期限切れ、ログアウト済み、BAN 済み、または形式が不正な場合は 401 を返す
- Note: 「refresh token が古くなったときだけ新しいものを発行する」更新は将来対応。その場合は、レスポンスに `refreshToken` を足すだけで、今の形との互換を保てる

### 自分の情報取得
- Get: /me
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"userId": "userid-80b0b29c-7514-40dd-a0b4-8f22f09329d8", "userName": "user1", "role": "user"}
```

### ログイン履歴
- Get: /login-history
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query(任意)
```json
{"limit": 20}
```
- Response (200)
```json
{
  "events": [
    {"kind": "login", "result": "failure", "reason": "wrong_password", "ip": "203.0.113.5", "userAgent": "Mozilla/5.0 ...", "at": "2026-11-15T21:00:00+09:00"},
    {"kind": "login", "result": "success", "reason": null, "ip": "203.0.113.4", "userAgent": "curl/8.5.0", "at": "2026-11-15T20:59:00+09:00"},
    {"kind": "signup", "result": "success", "reason": null, "ip": "203.0.113.4", "userAgent": "curl/8.5.0", "at": "2026-11-15T20:58:00+09:00"}
  ]
}
```
- Note: 自分(access token の持ち主)への、サインアップとログインの試行を、新しい順に返す。成功と失敗の両方で、不正なログインの試行に気づくために使う。ページングは無く、`limit` は 1〜100(既定 20)。範囲外は 400
- Note: `kind` は `"signup"` か `"login"`、`result` は `"success"` か `"failure"`、`reason` は失敗の理由で、`"wrong_password"` | `"locked"` | `"banned"` のいずれか。成功のときは null。理由は、本人にだけ見える(ログインのレスポンスでは、理由を区別しない)
- Note: 存在しないユーザー名への試行は、誰の履歴にも出ない(管理者の調査のために、DB には残る)。レート制限(429)に当たった試行は、攻撃で履歴が膨らまないよう、残さない
- Note: 履歴は、既定で180日残す(環境変数 `AUTH_LOGIN_HISTORY_RETENTION`)。ユーザーを削除すると、そのユーザーの履歴も消える
- Note: 管理者が、他のユーザーの履歴を見る API は、アカウント管理グループで定義する

### ログアウト (/logout)
- Post: /logout
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer <refreshToken>"
}
```
- Response (200)
```json
{}
```
- Note: 指定した refresh token の `jti` を無効化する。以後その refresh token では [アクセストークン (取得)](#アクセストークン-取得) を呼べない
- Note: ログアウト前に発行された access token は無効化されず、有効期限(最大15分)まで使える
- Note: 既にログアウト済みの refresh token を指定した場合は 401 を返す(無効な refresh token として扱う)

### JWKS 取得
- Get: /.well-known/jwks.json
- Header: なし(認証は不要)
- Response (200)
```json
{
  "keys": [
    {"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": "key-2026-11", "x": "<base64url の公開鍵>"}
  ]
}
```
- Note: 実際の URL は `/auth/.well-known/jwks.json`。バックエンドが access token を検証するための公開鍵を配信する
- Note: access token のヘッダの `kid` に一致する鍵で検証する。バックエンドは取得した鍵をキャッシュし、知らない `kid` が来たら取得し直す
- Note: 鍵を入れ替えるときは、古い鍵を、それで署名された access token が全て期限切れになる(15分)まで `keys` に残す

[← エンドポイント一覧](README.md)

## 認証関連 (/auth/)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/auth` を省いて書いている(実際の URL は `/auth/signup` など)。

### トークンの仕様
- access token: 有効期限は15分。署名は EdDSA(Ed25519)で、ヘッダの `kid` が公開鍵を指す。バックエンドは [JWKS 取得](#jwks-取得) で得た公開鍵でローカルに検証する。失効の管理はせず、有効期限だけで切れる(ログアウトや BAN の後も、発行済みのものは最大15分有効)
- refresh token: 有効期限は90日(固定)。更新(ローテーション)はしない。認証コンテナだけが発行・検証し、`jti` を DB で管理して有効・無効を判定する(ログアウトや BAN で無効になる)
- どちらも JWT。claims の例:
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
- Note: `userName` は英数字とハイフンのみ、最大64文字。大文字小文字を区別せずに重複を判定し、重複していたら 409 を返す
- Note: `password` は12文字以上。組み合わせ規則は課さず、よくあるパスワードと漏洩済みのパスワードは 400 で拒否する
- Note: 同じ IP からのサインアップは1時間あたり10回まで。超えたら 429(`Retry-After` 付き)を返す
- Note: サインアップの公開範囲(公開 / 招待制)は設定で切り替えられる。招待制のときの挙動は、アカウント管理グループで定義する

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
- Note: 上の回数と時間は仮の値で、設定で変えられる。ロックと BAN は認証基盤側で管理する(管理機能はアカウント管理グループで定義する)

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
{"userId": "userid-80b0b29c-7514-40dd-a0b4-8f22f09329d8", "userName": "user1"}
```

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

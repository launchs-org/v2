[← 認証関連](README.md)

# アクセストークン (取得)

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /refresh`

## 説明

[サインアップ](001-post-signup.md) / [ログイン](002-post-login.md) で発行された refresh token を使って、短命な access token(有効期限15分)を発行するエンドポイント。以後の各APIは原則この access token を `Authorization: Bearer <accessToken>` で付与して呼び出す。

refresh token は更新(ローテーション)しない。有効期限(90日)が切れるまで、同じ refresh token で何度でも access token を取得できる。トークンの仕様は [トークンの仕様](../../endpoints/auth.md#トークンの仕様) を参照。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 有効な refresh token を指定すると 200 が返り、access token(`type: "access"`、有効期限15分)が発行されること
- 発行された access token で [自分の情報取得](004-get-me.md) 等、他のAPIが呼び出せること
- 同じ refresh token で繰り返し呼び出しても、毎回新しい access token が返り、refresh token 自体は変わらないこと(レスポンスに refresh token は含まれない)
- 発行された access token が、[JWKS 取得](006-get-well-known-jwks.md) の公開鍵(ヘッダの `kid` に一致する鍵)で検証できること

### 異常系
- refresh token が無い、または不正な形式の場合 401 が返ること
- refresh token が期限切れ(発行から90日経過)の場合 401 が返ること
- access token を誤って `Authorization` に渡した場合(type が "access")、refresh用途には使えず 401 が返ること
- [ログアウト](005-post-logout.md) 済みの refresh token を指定した場合 401 が返ること
- BAN されたユーザーの refresh token を指定した場合 401 が返ること

### 境界値
- refresh token の `exp` を過ぎた瞬間から 401 になり、`exp` の前なら有効であること

## 仕様

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

## 参照

- [dev-docs/endpoints/auth.md](../../endpoints/auth.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

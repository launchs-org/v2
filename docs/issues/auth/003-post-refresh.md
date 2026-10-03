[← 認証関連](README.md)

# アクセストークン (取得)

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /refresh`

## 説明

[サインアップ](001-post-signup.md) / [ログイン](002-post-login.md) で発行された refresh token を使って、短命な access token を発行するエンドポイント。以後の各APIは原則この access token を `Authorization: Bearer <accessToken>` で付与して呼び出す。

## テスト要件

### 正常系
- 有効な refresh token を指定すると 200 が返り、access token(`type: "access"`)が発行されること
- 発行された access token で [自分の情報取得](004-get-me.md) 等、他のAPIが呼び出せること

### 異常系
- refresh token が無い、または不正な形式の場合 401 が返ること
- refresh token が期限切れの場合 401 が返ること
- access token を誤って `Authorization` に渡した場合(type が "access")、refresh用途には使えず 401 が返ること
- [ログアウト](005-post-logout.md) 済みの refresh token を指定した場合 401 が返ること

### 境界値
- refresh token の有効期限ぎりぎりでのリクエスト挙動(仕様未確定のため別途定義する)

## 仕様

- Post: /refresh
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Response (200)
```json
{
  "token": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJhY2Nlc3MifQ.EO1DFWzMQfJJf66tm897rDFMN2GbeSPT8-Q1jw5wE1HgNaeD_rgctCJ5J2ZHSTPo4Ar7KzcH2fdyLEawkCC9Hw"
}
```

## 参照

- [docs/endpoints/auth.md](../../endpoints/auth.md)

[← 認証関連](README.md)

# サインアップ

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /signup`

## 説明

新規ユーザーを登録し、認証用トークン(refresh token)を発行するエンドポイント。`userName`/パスワードを受け取り、ユーザーを作成した上でそのままログイン状態のトークンを返す(signup直後に別途loginを呼ぶ必要はない)。

## テスト要件

### 正常系
- 未使用の `userName` とパスワードで呼び出すと 200 が返り、`token`(refresh token)が発行されること
- 発行された `token` を使って [アクセストークン (取得)](003-post-refresh.md) が呼び出せること

### 異常系
- 既に使用されている `userName` を指定した場合、エラー(409 等)が返ること
- `userName` またはパスワードが未指定の場合 400 が返ること
- `Content-Type` が `application/json` 以外の場合 400 が返ること

### 境界値
- `userName` の最小長・最大長の境界(仕様未確定のため別途定義する)
- パスワードの最小強度要件(仕様未確定のため別途定義する)

## 仕様

- Post: /signup
- Header
```json
{"Content-Type" : "application/json"}
```
- Body
```json
{"userName" : "user1","Password1"}
```
- Response (200)
```json
{"token" : "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"}
```

## 参照

- [docs/endpoints/auth.md](../../endpoints/auth.md)

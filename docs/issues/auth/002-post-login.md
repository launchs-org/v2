[← 認証関連](README.md)

# ログイン

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /login`

## 説明

既存ユーザーの `userName`/パスワードを検証し、認証用トークン(refresh token)を発行するエンドポイント。

## テスト要件

### 正常系
- 登録済みの `userName` と正しいパスワードで呼び出すと 200 が返り、`token`(refresh token)が発行されること
- 発行された `token` を使って [アクセストークン (取得)](003-post-refresh.md) が呼び出せること

### 異常系
- 存在しない `userName` を指定した場合、401 が返ること(ユーザー存在有無を推測されないよう、パスワード誤りと同じエラーにする)
- パスワードが誤っている場合、401 が返ること
- `userName` またはパスワードが未指定の場合 400 が返ること

### 境界値
- 連続したログイン失敗に対するレート制限・アカウントロックの要否(仕様未確定のため別途定義する)

## 仕様

- Post: /login
- Header
```json
{"Content-Type": "application/json"}
```
- Body
```json
{"userName" : "user1","Password1"}
```
- Response (200)
```json
{
  "token": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```

## 参照

- [docs/endpoints/auth.md](../../endpoints/auth.md)

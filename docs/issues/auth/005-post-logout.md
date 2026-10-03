[← 認証関連](README.md)

# ログアウト (/logout)

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /logout`

## 説明

指定した refresh token を無効化するエンドポイント。ログアウト後はその refresh token で [アクセストークン (取得)](003-post-refresh.md) を呼び出せなくなる。

## テスト要件

### 正常系
- 有効な refresh token を指定すると 200 が返り、以後その refresh token で [アクセストークン (取得)](003-post-refresh.md) を呼ぶと 401 になること
- 既に発行済みの access token(ログアウト前に取得したもの)がログアウト後も有効期限内は使えるか/即座に無効化されるかは仕様未確定(別途定義する)

### 異常系
- `Authorization` ヘッダが無い、または不正な refresh token の場合 401 が返ること
- 既にログアウト済みの refresh token を再度指定した場合の挙動(401 or 200で冪等に成功扱い)は仕様未確定(別途定義する)

### 境界値
- 特になし

## 仕様

- Post: /logout
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Response (200)
```json
{}
```

## 参照

- [docs/endpoints/auth.md](../../endpoints/auth.md)

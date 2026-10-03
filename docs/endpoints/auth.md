[← エンドポイント一覧](README.md)

## 認証関連 (/auth/)
### サインアップ
- Post: /signup
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

### ログイン 
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

### アクセストークン (取得)
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
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Response (200)
```json
{}
```

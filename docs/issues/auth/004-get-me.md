[← 認証関連](README.md)

# 自分の情報取得

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Get /me`

## 説明

access token から自分自身(ログイン中のユーザー)の情報を取得するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 有効な access token を指定すると 200 が返り、`userId`/`userName` が取得できること

### 異常系
- `Authorization` ヘッダが無い場合 401 が返ること
- access token が無効・期限切れの場合 401 が返ること
- 署名が不正な access token(別の鍵で署名されたものなど)の場合 401 が返ること
- refresh token を誤って `Authorization` に渡した場合(type が "refresh")、401 が返ること

### 境界値
- ログアウトや BAN の後でも、発行済みの access token は有効期限(最大15分)まで使えること(access token の失効は管理しないため)

## 仕様

- Get: /me
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"userId": "userid-80b0b29c-7514-40dd-a0b4-8f22f09329d8", "userName": "user1"}
```

## 参照

- [docs/endpoints/auth.md](../../endpoints/auth.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

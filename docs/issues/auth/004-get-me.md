[← 認証関連](README.md)

# 自分の情報取得

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Get /me`

## 説明

access token から自分自身(ログイン中のユーザー)の情報を取得するエンドポイント。

## テスト要件

### 正常系
- 有効な access token を指定すると 200 が返り、`userId`/`userName` が取得できること

### 異常系
- `Authorization` ヘッダが無い場合 401 が返ること
- access token が無効・期限切れの場合 401 が返ること
- refresh token を誤って `Authorization` に渡した場合(type が "refresh")、401 が返ること

### 境界値
- 特になし(単純な参照系エンドポイントのため)

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

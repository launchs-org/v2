[← 認証関連](README.md)

# ログアウト (/logout)

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /logout`

## 説明

指定した refresh token を無効化するエンドポイント。ログアウト後はその refresh token で [アクセストークン (取得)](003-post-refresh.md) を呼び出せなくなる。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 有効な refresh token を指定すると 200 が返り、以後その refresh token で [アクセストークン (取得)](003-post-refresh.md) を呼ぶと 401 になること
- ログアウト前に発行された access token は無効化されず、有効期限(最大15分)まで使えること(access token の失効は管理しないため)

### 異常系
- `Authorization` ヘッダが無い、または不正な refresh token の場合 401 が返ること
- 既にログアウト済みの refresh token を再度指定した場合 401 が返ること(無効な refresh token として扱う)
- access token を誤って `Authorization` に渡した場合(type が "access")、401 が返ること

### 境界値
- 特になし

## 仕様

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
- Note: 指定した refresh token の `jti` を無効化する。以後その refresh token では [アクセストークン (取得)](003-post-refresh.md) を呼べない
- Note: ログアウト前に発行された access token は無効化されず、有効期限(最大15分)まで使える
- Note: 既にログアウト済みの refresh token を指定した場合は 401 を返す(無効な refresh token として扱う)

## 参照

- [docs/endpoints/auth.md](../../endpoints/auth.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

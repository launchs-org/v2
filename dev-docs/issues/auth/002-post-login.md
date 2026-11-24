[← 認証関連](README.md)

# ログイン

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /login`

## 説明

既存ユーザーの `userName`/`password` を検証し、認証用トークン(refresh token)を発行するエンドポイント。ブルートフォース対策として、ユーザー名単位のロックと IP 単位のレート制限を持つ。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 登録済みの `userName` と正しい `password` で呼び出すと 200 が返り、`token`(refresh token)が発行されること
- 発行された `token` を使って [アクセストークン (取得)](003-post-refresh.md) が呼び出せること
- ログインに成功すると、そのユーザー名の連続失敗の回数がリセットされること

### 異常系
- 存在しない `userName` を指定した場合 401 が返ること
- `password` が誤っている場合 401 が返ること
- 連続5回失敗したユーザー名は15分間ロックされ、ロック中は正しい `password` でも 401 が返ること
- 存在しないユーザー、パスワードの誤り、ロック中、BAN 中のいずれも、同じ 401(同じボディ)であること。アカウントの存在や状態をレスポンスから推測できないこと
- `userName` または `password` が未指定の場合 400 が返ること
- 同じ IP から1分間に20回を超えてログインを試行した場合 429 が返り、`Retry-After` ヘッダが付くこと

### 境界値
- 失敗が4回までは通常の 401 で、5回目の失敗でロックされること
- ロックから15分が経過したら、再びログインを試行できること
- 失敗の回数は、間に成功が入ると0に戻ること
- 回数と時間は仮の値で、設定で変えられること

## 仕様

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

## 参照

- [dev-docs/endpoints/auth.md](../../endpoints/auth.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

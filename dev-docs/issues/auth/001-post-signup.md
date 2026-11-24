[← 認証関連](README.md)

# サインアップ

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Post /signup`

## 説明

新規ユーザーを登録し、認証用トークン(refresh token)を発行するエンドポイント。`userName`/`password` を受け取り、ユーザーを作成した上でそのままログイン状態のトークンを返す(signup直後に別途loginを呼ぶ必要はない)。refresh token の仕様(有効期限90日、更新なし)は [トークンの仕様](../../endpoints/auth.md#トークンの仕様) を参照。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 未使用の `userName`(英数字とハイフン、64文字以内)と12文字以上の `password` で呼び出すと 200 が返り、`token`(refresh token)が発行されること
- 発行された `token` を使って [アクセストークン (取得)](003-post-refresh.md) が呼び出せること

### 異常系
- 既に使用されている `userName` を指定した場合 409 が返ること。大文字小文字だけが違う名前(`Alice` と `alice`)も重複として扱うこと
- `userName` または `password` が未指定の場合 400 が返ること
- `Content-Type` が `application/json` 以外の場合 400 が返ること
- `userName` に英数字とハイフン以外の文字が含まれる場合、または65文字以上の場合 400 が返ること
- `password` が12文字未満の場合 400 が返ること
- `password` がよくあるパスワード、または漏洩済みのパスワードの場合 400 が返ること
- 同じ IP から1時間に10回を超えてサインアップした場合 429 が返り、`Retry-After` ヘッダが付くこと

### 境界値
- `userName` がちょうど64文字の場合は許可され、65文字の場合は 400 になること(最小長は1文字以上。仮)
- `password` がちょうど12文字の場合は許可されること(よくあるパスワードでない場合)
- 招待制のときのサインアップの挙動は、アカウント管理グループで定義する(このグループでは扱わない)

## 仕様

- Post: /signup
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
- Note: token は refresh token。サインアップ後にそのままログイン状態になる
- Note: `userName` は英数字とハイフンのみ、最大64文字。大文字小文字を区別せずに重複を判定し、重複していたら 409 を返す
- Note: `password` は12文字以上。組み合わせ規則は課さず、よくあるパスワードと漏洩済みのパスワードは 400 で拒否する
- Note: 同じ IP からのサインアップは1時間あたり10回まで。超えたら 429(`Retry-After` 付き)を返す
- Note: サインアップの公開範囲(公開 / 招待制)は設定で切り替えられる。招待制のときの挙動は、アカウント管理グループで定義する

## 参照

- [dev-docs/endpoints/auth.md](../../endpoints/auth.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

[← 認証関連](README.md)

# ログイン履歴

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Get /login-history`

## 説明

自分(access token の持ち主)の、サインアップとログインの試行の履歴を、新しい順に返すエンドポイント。成功と失敗の両方が残り、不正なログインの試行に、本人が気づくために使う。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- サインアップ、ログインの成功、ログインの失敗(パスワードの誤り、ロック中、BAN 中)が、新しい順に返ること
- 各件に、`kind`、`result`、`reason`(失敗のときだけ)、`ip`、`userAgent`、`at`(JST)が入ること
- `limit` で件数を絞れること

### 異常系
- `Authorization` ヘッダが無い、access token が無効・期限切れの場合 401 が返ること
- refresh token を渡した場合 401 が返ること
- `limit` が範囲外(0 や 101)の場合 400 が返ること

### 境界値
- 他のユーザーへの試行、存在しないユーザー名への試行が、混ざらないこと
- レート制限(429)に当たった試行が、履歴に残らないこと
- 長い User-Agent は、256バイトに切り詰められること
- 180日(設定で変更可)より古い履歴が、掃除で消えること

## 仕様

[dev-docs/endpoints/auth.md](../../endpoints/auth.md#ログイン履歴) の「ログイン履歴」を参照。

## 参照

- [dev-docs/endpoints/auth.md](../../endpoints/auth.md)

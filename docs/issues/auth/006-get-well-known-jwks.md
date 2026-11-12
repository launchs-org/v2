[← 認証関連](README.md)

# JWKS 取得

- カテゴリ: 認証関連 (/auth/)
- Method / Path: `Get /.well-known/jwks.json`(実際の URL は `/auth/.well-known/jwks.json`)

## 説明

access token の署名を検証するための公開鍵(JWKS)を配信するエンドポイント。バックエンドが、認証コンテナに問い合わせずに access token をローカルで検証するために使う。署名アルゴリズムは EdDSA(Ed25519)。認証は不要。

## テスト要件

### 正常系
- 認証なしで呼び出すと 200 が返り、`keys` に現在の署名鍵(`kty: "OKP"`、`crv: "Ed25519"`、`alg: "EdDSA"`、`use: "sig"`)が含まれること
- [アクセストークン (取得)](003-post-refresh.md) で発行された access token のヘッダの `kid` が、`keys` のいずれかの `kid` に一致し、その公開鍵で署名を検証できること

### 異常系
- `keys` が空になる状態がないこと(起動時に必ず署名鍵を読み込む。読み込めない場合は起動しない)

### 境界値
- 鍵を入れ替えている間は、新旧の鍵が `keys` に両方含まれること
- 入れ替え前の鍵で署名された access token が、期限(15分)まで検証できること。古い鍵は、そのトークンが全て期限切れになるまで `keys` に残すこと
- バックエンドが知らない `kid` の access token を受け取ったとき、JWKS を取得し直して検証できること

## 仕様

- Get: /.well-known/jwks.json
- Header: なし(認証は不要)
- Response (200)
```json
{
  "keys": [
    {"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": "key-2026-11", "x": "<base64url の公開鍵>"}
  ]
}
```
- Note: 実際の URL は `/auth/.well-known/jwks.json`。バックエンドが access token を検証するための公開鍵を配信する
- Note: access token のヘッダの `kid` に一致する鍵で検証する。バックエンドは取得した鍵をキャッシュし、知らない `kid` が来たら取得し直す
- Note: 鍵を入れ替えるときは、古い鍵を、それで署名された access token が全て期限切れになる(15分)まで `keys` に残す

## 参照

- [docs/endpoints/auth.md](../../endpoints/auth.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

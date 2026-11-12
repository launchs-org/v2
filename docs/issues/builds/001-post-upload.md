[← ビルド関連](README.md)

# アップロード

- カテゴリ: ビルド関連 (/upload, /build)
- Method / Path: `Post /upload`

## 説明

ビルド対象のソースコードを zip でアップロードし、30分間有効な Asset トークンを発行するエンドポイント。このトークンを [ビルド (イメージ作成)](002-post-build.md) の `sourceType: "asset"` に渡すことで、アップロード済みのソースからビルドできる。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- MAX100MB 以内の zip ファイルをアップロードすると 200 が返り、`assetsToken` が発行されること
- 発行された `assetsToken` を [ビルド (イメージ作成)](002-post-build.md) の asset ソースとして指定してビルドできること
- `assetsToken` のペイロードに `assetId`/`sha256`/`type: "asset"` が含まれ、改ざん検知に使えること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- zip 以外のファイル形式を送った場合 400 が返ること
- zipの拡張子やContent-Typeを装っていても、実体が破損している・zip形式として解釈できないファイルの場合は 400 で拒否すること(マジックバイトやzip構造の検証をサーバ側で行い、クライアント申告のファイル名/Content-Typeだけを信用しない)
- 100MB を超えるファイルを送った場合 413 が返ること
- 発行から30分経過した `assetsToken` を [ビルド (イメージ作成)](002-post-build.md) で使用した場合エラーになること
- アップロードした zip が共有ボリュームに保存され、30分の期限が切れたら自動で削除されること

### 境界値
- ちょうど100MBのファイルの扱い(以下は許可/超過は拒否の境界)
- 空(0バイト)のzipファイルを送った場合の挙動(仕様未確定のため別途定義する)
- zipとして一見正しく開けるが、不正なパス(`../`を含むパストラバーサル等)を含むエントリがある場合は展開前に検出して拒否すること

## 仕様

- Post: /upload
- Header
```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsInN1YiI6InVzZXJpZC04MGIwYjI5Yy03NTE0LTQwZGQtYTBiNC04ZjIyZjA5MzI5ZDgiLCJuYW1lIjoidXNlcjEiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsInR5cGUiOiJyZWZyZXNoIn0.J3V-cB-UJx0NHTHif58Jp1yfXW7WEWOjkJU0Y7EFDCx8FTttbEQwtX1G--pkqXWKj1ObztYX4O8HucFkxvtDNw"
}
```
- Request
- zip でボディを送信する (MAX100MB)
- Response (200)
- Note: 30分間有効なAssetトークンを吐き出す これをビルドに渡してダウンロードを行う
- Note: アップロードされた zip は、バックエンドとビルドの両方から参照できる共有ボリュームに保存され、期限(30分)が切れたら自動で削除される。期限切れの assetsToken を使うとエラーになる
- Note: zipとして不正なファイル(拡張子/Content-Typeを偽装していても実体が壊れている、zip形式として解釈できない、不正なパスを含むエントリがある等)は400で拒否する
```json
{
  "assetsToken": "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJsYXVuY2hzLW9yZyIsImV4cCI6IjE1MTYyMzkwMjIiLCJqdGkiOiJ0b2tlbmlkLWQ5Y2M2NGFiLTIxNmUtNDYwYy1iNThjLTRiNWNmMzZhODI5NSIsImFzc2V0SWQiOiJhc3NldC0wMmVlYjFmOC0yODRlLTRiZGItODUxNi1jM2QzMzdlMzk0NjAiLCJzaGEyNTYiOiI5ODM0ODc2RENGQjA1Q0IxNjdBNUMyNDk1M0VCQTU4QzRBQzg5QjFBREY1N0YyOEYyRjlEMDlBRjEwN0VFOEYwIiwidHlwZSI6ImFzc2V0In0.6YQ16LRR0E5JcTIFdFrBcsttm6ki0HWmgmju5irGo9XJWkcCEXqD7MXmVQ7II_Y2ydA29yCwp96VIvX2k78_3g"
}
```

## 参照

- [docs/endpoints/builds.md](../../endpoints/builds.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

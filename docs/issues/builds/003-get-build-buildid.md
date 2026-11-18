[← ビルド関連](README.md)

# ビルド状態取得

- カテゴリ: ビルド関連 (/upload, /build)
- Method / Path: `Get /build/:buildId`

## 説明

[ビルド (イメージ作成)](002-post-build.md) で開始したビルドの進行状況(`status`)を確認するエンドポイント。非同期ビルドの完了をポーリングするために使う。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- ビルド開始直後は `status: "pending"` または `"running"` が返ること
- ビルドが完了すると `status: "success"` になり、[ビルド (イメージ作成)](002-post-build.md) の時点と同じ `imageRef` が返ること
- ビルドが失敗すると `status: "failed"` になること
- [ビルドキャンセル](005-post-build-buildid-cancel.md) を呼ぶと `status` が `"canceling"` を経て `"canceled"` になること
- [ビルドキャンセル](005-post-build-buildid-cancel.md) で `"canceling"` になった後、中断より先にビルドが完了した場合は `"canceled"` にはならず、先に確定した `"success"`/`"failed"` のままになること

### 異常系
- 存在しない `buildId` を指定した場合 404 が返ること
- 他ユーザーが所有する `buildId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `status` は "pending" | "running" | "canceling" | "success" | "failed" | "canceled" 以外の値を取らないこと

## 仕様

- Get: /build/:buildId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "status": "running",
  "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx"
}
```
- Note: status は "pending" | "running" | "canceling" | "success" | "failed" | "canceled" のいずれか
- Note: imageRef は [ビルド (イメージ作成)](002-post-build.md) の時点で確定し、以後不変(build成功を待たずに参照可能。ただし実際にpullできるのは status が "success" になった後)

## 参照

- [docs/endpoints/builds.md](../../endpoints/builds.md)
- [docs/endpoints/common.md](../../endpoints/common.md)

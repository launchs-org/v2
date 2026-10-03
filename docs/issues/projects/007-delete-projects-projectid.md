[← プロジェクト関連](README.md)

# プロジェクト削除

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Delete /projects/:projectId`

## 説明

プロジェクトを削除するエンドポイント。配下の build 履歴・イメージ・deploy(稼働中のものを含む)は全て連鎖して削除される。

## テスト要件

### 正常系
- 自分が所有する `projectId` を指定すると 200 が返り、以後 [プロジェクト取得](003-get-projects-projectid.md) で 404 になること
- 削除後、配下の build 履歴・イメージ・稼働中だった deploy(およびそのservice/volumeマウント)が全て連鎖削除されていること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 稼働中の deploy が多数ある場合の連鎖削除の完了タイミング(同期/非同期)は仕様未確定のため別途定義する
- 配下の volume(永続ストレージ)自体も連鎖削除されるのか、ボリュームは残すのかは仕様未確定のため別途定義する

## 仕様

- Delete: /projects/:projectId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: プロジェクト削除時、配下の build 履歴・イメージ・deploy(稼働中のものを含む)は全て連鎖して削除される

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)

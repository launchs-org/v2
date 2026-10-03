[← エンドポイント一覧](README.md)

## プロジェクト関連 (/projects)
### プロジェクト作成
- Post: /projects
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "my-app"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
```
- Note: build/deploy は必ずいずれかの projectId に属する。以後の /build /deploy は projectId を伴って呼び出す

### プロジェクト一覧
- Get: /projects
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projects": [{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app"}]}
```

### プロジェクト取得
- Get: /projects/:projectId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app"}
```

### プロジェクト build 履歴一覧
- Get: /projects/:projectId/builds
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "builds": [
    {
      "buildId": "build-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "success",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T00:00:00Z"
    }
  ]
}
```
- Note: 作成日時の降順で返す(ページングは未設計・別途)

### プロジェクト deploy 履歴一覧
- Get: /projects/:projectId/deploys
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "deploys": [
    {
      "deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "active",
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T00:00:00Z"
    }
  ]
}
```
- Note: 作成日時の降順で返す(削除済みdeployを含めるかは未設計・別途)。ページングは未設計・別途

### プロジェクト更新
- Patch: /projects/:projectId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "my-app-renamed"}
```
- Response (200)
```json
{}
```

### プロジェクト削除
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

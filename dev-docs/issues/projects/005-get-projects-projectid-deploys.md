[← プロジェクト関連](README.md)

# プロジェクト deploy 履歴一覧

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Get /projects/:projectId/deploys`

## 説明

指定したプロジェクト配下の deploy 履歴を、作成日時の降順で取得するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- deploy を1件も実行していないプロジェクトの場合、空配列が返ること
- 複数回 deploy した場合、`createdAt` の降順(新しい順)で返ること
- 各要素に `deployId`/`status`/`spec`/`replicas`/`imageRef`/`createdAt` が含まれること。起動設定(環境変数など)は含まれないこと
- 作成日時が同じ deploy が複数あっても、ID で順序が決まり、ページをまたいで、重複も欠落も無いこと

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 削除が完了した deploy は一覧に含まれないこと。削除中(status が `"deleting"`)の deploy は含まれること(仮)
- `limit` を指定すると、その件数以内で返り、続きがあれば `nextCursor` が入ること
- `nextCursor` を `cursor` に渡すと次のページが返り、重複や欠落がないこと
- 最後のページで `nextCursor` が null になること
- 範囲外の `limit`・不正な `cursor` は 400 が返ること(仮)

## 仕様

- Get: /projects/:projectId/deploys
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query(どちらも任意)
```json
{"limit": 20, "cursor": "<前回の nextCursor>"}
```
- Response (200)
```json
{
  "deploys": [
    {
      "deployId": "deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "status": "active",
      "spec": "small",
      "replicas": 1,
      "imageRef": "registry.launchs-org.internal/project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:build-xxxxxxxx",
      "createdAt": "2026-09-16T09:00:00+09:00"
    }
  ],
  "nextCursor": null
}
```
- Note: `spec`(スペックの名前)と `replicas`(希望するレプリカ数)も返す(一覧で、deploy の大きさが分かるように)。起動設定(環境変数など)や、Pod の状態は、含まない([デプロイ詳細取得](deploys.md#デプロイ詳細取得) で取る)
- Note: 作成日時の降順で返す。`limit` は1〜100(デフォルト20)。範囲外の `limit` と不正な `cursor` は 400(仮)。`cursor` はクライアントが解釈しない文字列で、`nextCursor` は最後のページなら null、続きがあれば文字列
- Note: deploy は削除が完了するとレコードごと消えるため、削除済みの deploy は含まれない。削除中(status が "deleting")のものは含まれる(仮)

## 参照

- [dev-docs/endpoints/projects.md](../../endpoints/projects.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)

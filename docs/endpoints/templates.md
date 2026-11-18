[← エンドポイント一覧](README.md)

## テンプレート関連 (/templates)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。このグループのパスは `/api/v1` を省いて書いている(実際の URL は `/api/v1/templates` など)。設計は [docs/templates.md](../templates.md)。

**この API は、一覧と取得だけ。** テンプレートから部品を作る API(`POST /projects/:projectId/templates/:templateId/apply`)は、段階 C で作る。

### テンプレート一覧
- Get: /templates
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "templates": [
    {
      "id": "postgres",
      "name": "PostgreSQL",
      "description": "データベース(PostgreSQL 16)。データは、ボリュームに保存されます。",
      "icon": "database",
      "category": "database",
      "variables": [
        {"name": "database", "label": "データベース名", "type": "string", "default": "app", "required": false, "generate": false, "options": [], "min": null, "max": null, "minLength": null, "maxLength": null, "pattern": "^[a-z][a-z0-9_]{0,30}$"},
        {"name": "password", "label": "パスワード", "type": "password", "default": null, "required": false, "generate": true, "options": [], "min": null, "max": null, "minLength": null, "maxLength": null, "pattern": null},
        {"name": "storage", "label": "ボリュームの大きさ(MB)", "type": "integer", "default": "1024", "required": false, "generate": false, "options": [], "min": 256, "max": 5120, "minLength": null, "maxLength": null, "pattern": null}
      ],
      "summary": {"deploys": 1, "volumes": 1, "ingress": false, "files": 1}
    }
  ]
}
```
- Note: テンプレートは、このシステムに同梱の YAML(リポジトリの `templates/*.yaml`)を、backend の起動時に読み込んだもの。データベースには無く、**全員に同じものを返す**。利用者が、定義を、追加や変更することはできない
- Note: 並びは `id` の辞書順
- Note: `variables` は、入力欄の定義。`type` は `"string"` | `"integer"` | `"password"` | `"choice"` | `"boolean"`。値は、全て、文字列として扱う(`integer` の既定値も `"1024"` のような文字列)
- Note: `required` は、入力が必須か(既定値が無く、自動生成でもない)。`required` が `true` の変数は、作るときに、必ず、値が要る
- Note: **`generate` が `true` の `password` は、`default` を、返さない(常に `null`)**。入力が無ければ、作るときに、サーバーが自動で作る(英数字。既定 24 文字)
- Note: `options` は、`choice` の選択肢(`{"value", "label"}` の配列)。他の型では、空の配列。`min` / `max` は `integer`、`minLength` / `maxLength` は `string` と `password`、`pattern` は `string`(値の全体に一致する正規表現)の制約。無ければ `null`
- Note: `summary` は、何が作られるかの要約(deploy と volume の数、ingress を作るか、deploy に置くファイルの合計の数)。**作る部品の中身(`resources`)は、返さない**
- Note: `icon` は、画面の絵の名前。`category` は `"database"` | `"web"` | `"cache"` | `"tool"`、または空文字

### テンプレート取得
- Get: /templates/:templateId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200): 一覧の1件と同じ形(`templates` の配列の要素)
- Note: 存在しない `templateId` は 404

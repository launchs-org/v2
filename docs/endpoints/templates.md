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
      "notes": "",
      "version": "1.0.0",
      "versions": ["1.0.0"],
      "variables": [
        {"name": "database", "label": "データベース名", "type": "string", "default": "app", "required": false, "generate": false, "options": [], "min": null, "max": null, "minLength": null, "maxLength": null, "pattern": "^[a-z][a-z0-9_]{0,30}$"},
        {"name": "password", "label": "パスワード", "type": "password", "default": null, "required": false, "generate": true, "options": [], "min": null, "max": null, "minLength": null, "maxLength": null, "pattern": null},
        {"name": "storage", "label": "ボリュームの大きさ(MB)", "type": "integer", "default": "1024", "required": false, "generate": false, "options": [], "min": 256, "max": 5120, "minLength": null, "maxLength": null, "pattern": null}
      ],
      "summary": {"deploys": 1, "volumes": 1, "ingress": false, "files": 1, "configmaps": 1}
    }
  ]
}
```
- Note: テンプレートは、このシステムに同梱のフォルダ(別リポジトリ [launchs-v2-template](https://github.com/launchs-org/launchs-v2-template) を、`templates/` に取り込んだもの。構成は `manifest.yaml` + `README.md` + 版のフォルダ)を、backend の起動時に読み込んだもの。データベースには無く、**全員に同じものを返す**。利用者が、定義を、追加や変更することはできない
- Note: 並びは `id` の辞書順。**一覧は、各テンプレートの、既定の版**(`manifest.yaml` の `latest`。無ければ、いちばん新しい版)の内容を返す
- Note: `version` は、この応答の版(`x.y.z`)、`versions` は、選べる全ての版(新しい順)。版ごとに、変数や案内が違いうる
- Note: `variables` は、入力欄の定義。`type` は `"string"` | `"integer"` | `"password"` | `"choice"` | `"boolean"`。値は、全て、文字列として扱う(`integer` の既定値も `"1024"` のような文字列)
- Note: `required` は、入力が必須か(既定値が無く、自動生成でもない)。`required` が `true` の変数は、作るときに、必ず、値が要る
- Note: **`generate` が `true` の `password` は、`default` を、返さない(常に `null`)**。入力が無ければ、作るときに、サーバーが自動で作る(英数字。既定 24 文字)
- Note: `options` は、`choice` の選択肢(`{"value", "label"}` の配列)。他の型では、空の配列。`min` / `max` は `integer`、`minLength` / `maxLength` は `string` と `password`、`pattern` は `string`(値の全体に一致する正規表現)の制約。無ければ `null`
- Note: `summary` は、何が作られるかの要約(deploy、volume、ConfigMap の数、ingress を作るか、deploy に置くファイルの合計の数)。**作る部品の中身(`resources`)は、返さない**
- Note: `icon` は、画面の絵の名前。`category` は `"database"` | `"web"` | `"cache"` | `"tool"` | `"monitoring"` | `"dev"`、または空文字。`notes` は、作成後に出す案内(`${変数名}` を含みうる。無ければ空文字)

### テンプレート取得
- Get: /templates/:templateId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Query: `version`(`x.y.z`。省略は、既定の版)
- Response (200): 一覧の1件と同じ形(`templates` の配列の要素)に、`readme`(テンプレートの README.md。無ければ省略)が加わる。**一覧は、`readme` を返さない**
- Note: 存在しない `templateId`、存在しない `version` は 404

### テンプレートから作る
- Post: /projects/:projectId/templates/:templateId/apply
- Header
```json
{"Authorization": "Bearer <accessToken>", "Content-Type": "application/json"}
```
- Body (`values` は省略できる。省略した変数は、既定値か、自動生成になる。`version` も省略できる。省略は、既定の版)
```json
{"version": "1.0.0", "values": {"database": "app", "storage": "2048"}}
```
- Response (200)
```json
{"applyId": "apply-xxxxxxxx-...", "version": "1.0.0", "values": {"database": "app", "user": "app", "password": "…(自動生成)", "storage": "2048"}}
```
- Note: **受け付けるだけで、すぐ応答する**。部品は、非同期で、順に作る。進み具合は、下の API で取る
- Note: `version` は、**使った版**(省略したときは、既定の版)。実行の記録にも残る。存在しない版は 404(何も保存しない)。変数の検査は、**その版の定義**で行う
- Note: `values` は、**自動生成を含む、全ての変数の値**(画面が、パスワードを見せるため)。パスワードも平文で返す(env と同じ扱い)
- Note: 受け付けのとき、変数の検査、展開、**クォータの全体での確認**を行う。1 つでも通らなければ、**何も作らない**
- Note: 400: 変数の値が不正(型、範囲、パターン)、テンプレートに無い変数、展開できない。404: プロジェクトかテンプレートが無い(他のユーザーのプロジェクトも)。409: プロジェクトが active でない、クォータを超える(deploy の数、スペックごとの数、volume の数と大きさの合計)、ConfigMap がプロジェクトの上限(50)を超える
- Note: 作る順序は、volume → ConfigMap → deploy と、その service(`${service.X.dns}` で参照される側が先)→ ingress。deploy は、**最初から、マウントとファイルを持った形**で作る(最初の起動から、ファイルが読める)
- Note: 部品の名前(volume、ConfigMap)は、`<テンプレートの id>-<部品の name>-<実行の ID の先頭 6 文字>`(同じテンプレートを、何度でも作れる)

### テンプレートの実行の進み具合
- Get: /projects/:projectId/template-applies/:applyId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{
  "applyId": "apply-xxxxxxxx-...",
  "templateId": "wordpress",
  "templateVersion": "1.0.0",
  "status": "running",
  "values": {"db_password": "…", "db_storage": "1024"},
  "steps": [
    {"kind": "volume", "name": "dbdata", "status": "done", "resourceId": "volume-...", "error": null},
    {"kind": "deploy", "name": "db", "status": "creating", "resourceId": "deploy-...", "error": null},
    {"kind": "service", "name": "db", "status": "waiting", "resourceId": "", "error": null}
  ],
  "error": null,
  "createdAt": "2026-11-18T16:54:05+09:00",
  "updatedAt": "2026-11-18T16:54:13+09:00"
}
```
- Note: `templateVersion` は、使った版(版を扱う前の記録は、空文字)
- Note: `status` は `"running"`(作成中)| `"succeeded"`(全て完了)| `"failed"`(どれかが失敗)
- Note: `steps` は、**作る順**。`kind` は `"volume"` | `"configmap"` | `"deploy"` | `"service"` | `"ingress"`、`name` は、テンプレートの中の部品の名前(service は、その deploy の name)。`status` は `"waiting"` | `"creating"` | `"done"` | `"failed"`。`resourceId` は、作り始めたら入る
- Note: deploy と service は、起動(active)を待ってから `"done"` になる
- Note: **途中で失敗したら、そこで止まる**。作れたものは、**そのまま残す**(自動では消さない)。失敗したステップの `error` と、全体の `error` に、理由が入る。利用者は、残りを手で直すか、消す
- Note: 404: 実行が無い、他のユーザーのもの、別のプロジェクトのもの

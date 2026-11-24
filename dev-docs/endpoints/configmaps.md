# ConfigMap 関連(/projects/:projectId/configmaps, /configmaps/:configMapId)

プロジェクトで共有する、設定ファイルの組。設計は [../configmap.md](../configmap.md)。共通のルール(ID、日時、エラー形式、404、ページング)は [common.md](common.md)。

状態: **段階 1(ConfigMap の資源)と、段階 2(deploy へのマウント。[deploys.md](deploys.md) の `/deploy/:deployId/configmap-mounts`)は実装済み**。画面は、段階 3。

ConfigMap の編集は、新しい版を DB に足すだけで、k8s には触れない。そのため、pending も apply も無い。

## ファイル 1 つの形

```json
{"key": "app.conf", "encoding": "text", "content": "a=b\n"}
```

- `key`: ファイル名。`[A-Za-z0-9._-]+`、253 文字まで。`.` と `..` は不可。`/` は含められない(階層は、マウントの path で作る)。
- `encoding`: `text`(UTF-8 で、NUL 文字を含まない)か `base64`(バイナリ)。**省略は `text`**。
- `content`: `text` はそのままの文字列。`base64` は標準の base64(パディングあり。改行と空白は不可)。空でもよい。

## 制限

- 保存する大きさ(key の長さ + 内容の長さ。base64 は、base64 にした後の長さ)が、ファイル 1 つでも、ConfigMap 全体でも、**1 MiB** まで。超えると 400。
- ファイルは 50 個まで。1 プロジェクトに 50 個の ConfigMap まで(超えると 409)。
- 作成と置き換えのリクエストの本文は 3 MiB まで(超えると 413)。
- 版は、直近 10 版と、**どれかの deploy が使っている版**(現在と pending)を残す。それ以外は、編集のときに消える。

## POST /projects/:projectId/configmaps

作成する。版 1 ができる。

Body: `{"name": "app-config", "files": [{"key": "app.conf", "content": "a=b\n"}]}`(`files` は省略または空でもよい)

Response 200: 下の「ConfigMap 1 件」の形。`files` は key の昇順。

- 400: name が、英数字とハイフンだけの 1〜64 文字でない。files が不正(key、encoding、base64、大きさ、重複、個数)。
- 404: プロジェクトが無い、または他のユーザーのもの。
- 409: 同じプロジェクトに同じ名前がある。プロジェクトが active でない。ConfigMap が 50 個ある。

## GET /projects/:projectId/configmaps

一覧(新しい順。`limit`、`cursor`)。**中身は返さない**。

```json
{"configmaps": [{"configMapId": "configmap-...", "name": "app-config", "currentRevision": 3, "fileCount": 2, "totalBytes": 120, "usedBy": 1, "updatedAt": "2026-11-18T13:00:00+09:00"}], "nextCursor": null}
```

`totalBytes` は、最新の版の、保存する大きさの合計。`usedBy` は、使っている deploy の数(pending のマウントも含む)。404: プロジェクトが無い、または他のユーザーのもの。

## GET /configmaps/:configMapId

最新の版の中身。`?revision=N` で、その版の中身。

```json
{"configMapId": "configmap-...", "projectId": "project-...", "name": "app-config", "currentRevision": 3, "revision": 3,
 "files": [{"key": "app.conf", "encoding": "text", "content": "a=b\n", "size": 4}],
 "createdAt": "...", "updatedAt": "..."}
```

`size` は、元のバイト数(base64 はデコードした大きさ)。`revision` は、`files` がどの版か。

- 400: `revision` が、1 以上の整数でない。
- 404: ConfigMap が無い、他のユーザーのもの、または、その版が無い(消えた版を含む)。

## PUT /configmaps/:configMapId

ファイルを**全件置き換える**(個別のファイルの操作は無い)。

Body: `{"files": [...]}`(`files` は必須。空の配列は「全て外す」)

Response 200: 操作の後の最新の版(「ConfigMap 1 件」の形)。

- 中身が、最新の版と同じ(順番が違っても同じ)なら、**版を増やさず** 200。違えば、新しい版(+1)。
- 400: files が不正、または `files` の省略。404: 無い、または他のユーザーのもの。

## PATCH /configmaps/:configMapId

名前を変える。Body: `{"name": "..."}`。Response 200: `{}`。400: 名前が不正。404: 無い。409: 同じ名前がある。

## DELETE /configmaps/:configMapId

全ての版ごと削除する。Response 200: `{}`。404: 無い、または他のユーザーのもの。
409: どれかの deploy が使っている(pending のマウントも含む)。メッセージに、使っている deploy の ID が入る。使っている deploy が、マウントを外して(適用するか、pending を破棄して)から、消す。

## GET /configmaps/:configMapId/revisions

残っている版の一覧(新しい順)。

```json
{"revisions": [{"revision": 3, "fileCount": 2, "totalBytes": 120, "createdAt": "..."}]}
```

404: 無い、または他のユーザーのもの。

## GET /configmaps/:configMapId/usage

使っている deploy のマウント(現在と pending の両方)。

```json
{"usages": [{"deployId": "deploy-...", "revision": 1, "key": "app.conf", "path": "/etc/app/app.conf", "pending": false}]}
```

`pending` は、まだ適用していないマウントなら true。404: 無い、または他のユーザーのもの。

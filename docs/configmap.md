# ConfigMap(共有できる設定ファイル)の設計

状態: **段階 1(ConfigMap の資源。API、版、テスト)、段階 2(deploy へのマウント、worker)、段階 3(画面)は実装済み**。確認待ちの点は §12。API の確定仕様は [endpoints/configmaps.md](endpoints/configmaps.md)。

プロジェクトの資源として ConfigMap を作り、複数の deploy から、ファイルとしてマウントできるようにする。画面からファイルを作る・アップロードする・編集する。バイナリも扱う。ファイルごとに権限(mode)を決められる。

関連: deploy ごとの「ファイル」([endpoints/deploys.md](endpoints/deploys.md) の `/deploy/:id/files`。[templates.md](templates.md) の段階 A)は、すでに実装済み。これは**1つの deploy だけが持つ、テキストのファイル**。この設計は、それとは別に、**プロジェクトで共有する資源**を足す(§10)。

## 1. 決めたこと

| 項目 | 決定 |
|---|---|
| 持ち主 | **プロジェクト**(volume、ingress と同じ。`/projects/:projectId/configmaps`) |
| 共有 | 複数の deploy が、同じ ConfigMap をマウントできる |
| 中身 | ファイルの組(キー = ファイル名、値 = 内容)。**テキストと、バイナリ**(base64 で送る) |
| マウント | **ファイル1つずつ**、deploy の好きな path に(`subPath`)。ファイルごとに **mode**(権限)を指定できる |
| 更新 | ConfigMap を編集すると、**新しい版(revision)**ができる。**deploy は、版を固定してマウントする**。deploy が、自分で適用し直したときだけ、新しい版になる(§3) |
| 秘密 | 今回は扱わない。平文で保存し、そのまま返す(env と同じ。[templates.md](templates.md) の未解決 1) |

## 2. モデル

### configmaps

| 列 | 型 | 意味 |
|---|---|---|
| id | text | `configmap-<uuid>` |
| project_id | text | 持ち主のプロジェクト |
| name | text | 表示名。プロジェクトの中で一意。volume の name と同じ規則 |
| current_revision | int | 最新の版の番号(1 から) |
| created_at / updated_at | | |

### configmap_revisions

| 列 | 型 | 意味 |
|---|---|---|
| configmap_id | text | |
| revision | int | 版の番号。1 から、編集のたびに +1 |
| files | jsonb | `[{"key": "app.conf", "encoding": "text", "content": "..."}, ...]`。`encoding` は `text` か `base64` |
| created_at | | |

- 版の行は、**更新できない**(追記だけ)。編集は、新しい行を足す。
- 残す版: 最新の版と、**どれかの deploy が使っている版**(適用済み、pending の両方)と、直近 10 版。それ以外は、編集のときに消す。

### deploy 側

deploys に、deploy ごとの列を足す(`mounts`、`pending_mounts` と同じ形。マイグレーションは up だけ)。

- `configmap_mounts`: 現在コンテナに反映されている内容
- `pending_configmap_mounts`: まだ適用していない内容(null = 変更なし。空の配列 = 全て外す)

要素は、次の形。

```json
{"configMapId": "configmap-xxxxxxxx-...", "revision": 3, "key": "app.conf", "path": "/etc/app/app.conf", "mode": "0644"}
```

- 1つの要素 = ConfigMap の 1 ファイルを、1 つの path にマウントする(同じファイルを、2 か所にマウントしてもよい)。
- `revision` は、**保存するときに、サーバーが最新の版で埋める**(利用者が決めなくてよい)。すでに同じ ConfigMap の同じ key をマウントしていて、利用者が版を変えたくないとき(他の項目だけ変えた)は、今の版を保つ(§3)。
- `mode` は、8 進数の 4 桁の文字列(`"0000"` から `"0777"`)。省略は `"0644"`。

## 3. 更新の考え方(いちばん大事なところ)

ConfigMap の 1 ファイルを `subPath` でマウントすると、ConfigMap を更新しても、**動いている Pod には反映されない**(Pod を作り直す必要がある。[templates.md](templates.md) の段階 A と同じ事情)。そこで、次の形にする。

1. **ConfigMap を編集する** = 新しい版(revision)を、DB に足すだけ。k8s には、何も起きない。使っている deploy にも、影響しない。
2. **deploy が、版を固定している**。deploy の画面には、「ConfigMap `app-config` に、新しい版があります(3 → 5)」と出す。
3. 利用者が、deploy の ConfigMap マウントを、保存し直して(最新の版にして)、適用する。そのとき初めて、新しい版が、k8s の ConfigMap になり、Pod が作り直される。
4. k8s の ConfigMap の名前は、**`<configMapId>-r<revision>`**、`immutable: true` で作る。適用が失敗したら、Deployment を前の版に戻すだけでよい(前の版の ConfigMap は、まだ残っている)。

この形にすると、次のことが、きれいに解ける。

- 共有している ConfigMap を直しても、**使っている全ての deploy が、一斉に再起動することは無い**(影響を、1 つずつ、自分の都合で、確かめながら広げられる)。
- 失敗の巻き戻しが、簡単(版を戻すだけ)。
- 適用の仕組みは、既存の mounts、files と同じ「pending → 適用 → 結果の確定」のまま。

**この形では、ConfigMap 自体の編集に、pending も apply も無い**(k8s に反映するものが無いため)。docs/endpoints の「pending → apply」のルールは、**インフラへの反映があるもの**に当てはまる。

古い版の k8s の ConfigMap は、deploy が使わなくなったとき(適用の成功のあと)に、worker が消す。どの deploy も使っていない版は、残さない。

## 4. 制限

| 項目 | 上限 | 理由 |
|---|---|---|
| ファイル 1 つの、保存する大きさ | 1 MiB | 保存する大きさ = key の長さ + 内容の長さ(base64 は、base64 にした後の文字列の長さ) |
| ConfigMap 1 つの、保存する大きさの合計 | 1 MiB | k8s の ConfigMap は、全体で 1 MiB まで。保存する大きさで数えれば、必ず下回る。元のバイトでは、バイナリ 約 730 KB、テキスト 約 1 MB まで |
| ConfigMap 1 つの、ファイルの数 | 50 | |
| key(ファイル名) | `[A-Za-z0-9._-]+`、253 文字まで。`.` と `..` は不可 | k8s の ConfigMap のキーの規則 |
| 1 プロジェクトの ConfigMap の数 | 50(定数。アカウントごとのクォータには、まだ入れていない) | |
| 1 deploy の ConfigMap マウントの数 | 30 | |
| 1 deploy の、(ConfigMap のマウント + files)の ファイルの数 | 合計 40 | Deployment の大きさ |

- 作成と置き換えの API だけ、本文の上限を 3 MiB にする(text の改行や引用符は、JSON で 2 文字になるため)。超えると 413。
- バイナリは `encoding: "base64"` で送る。サーバーは、base64 を検査し、元のバイト数で、上限を数える。`text` は、UTF-8 で、NUL 文字を含まないこと(今の deploy のファイルと同じ)。

## 5. API の案

[endpoints/volumes.md](endpoints/volumes.md) と同じ形。共通のルールは [endpoints/common.md](endpoints/common.md)。

### ConfigMap の管理(プロジェクトの下)

| Method / Path | 内容 |
|---|---|
| `POST /projects/:projectId/configmaps` | 作る。`{"name": "...", "files": [...]}`。版 1 ができる。名前の重複は 409 |
| `GET /projects/:projectId/configmaps` | 一覧(ページング)。中身は返さず、`id`、`name`、`currentRevision`、`fileCount`、`totalBytes`、`updatedAt`、`usedBy`(使っている deploy の数) |
| `GET /configmaps/:configMapId` | 取得。最新の版の中身。`?revision=N` で、その版(base64 の中身を含む) |
| `PUT /configmaps/:configMapId` | **全件置き換え**(今の deploy のファイルと同じ。個別のファイルの CRUD は作らない)。`{"files": [...]}`。**現在の版と中身が同じなら、版を増やさず 200**。違えば、新しい版(+1) |
| `PATCH /configmaps/:configMapId` | 名前の変更(`{"name": "..."}`) |
| `DELETE /configmaps/:configMapId` | 削除。**どれかの deploy が、使っている(pending のマウントも含む)と 409**。使っている deploy の一覧を、エラーの本文に入れる |
| `GET /configmaps/:configMapId/revisions` | 版の一覧(番号、作成時刻、合計の大きさ、使っている deploy の数) |
| `GET /configmaps/:configMapId/usage` | 使っている deploy の一覧(deploy の ID、名前、マウントの path、**使っている版**、pending かどうか) |

- 画面でファイルを開くたびに、中身が全て返ると重いので、一覧には中身を入れない。取得は、`?include=content` を付けたときだけ中身を返す案もある(§12 の 4)。
- ConfigMap の編集は、使っている deploy の status を見ない(deploy の `applying` の間も、できる。反映するものが、無いため)。

### deploy へのマウント(`/deploy/:deployId/mounts` と同じ形)

| Method / Path | 内容 |
|---|---|
| `GET /deploy/:deployId/configmap-mounts` | `{"mounts": [...], "pending_mounts": null}`。各要素に、`latestRevision`(ConfigMap の今の最新の版)も入れる(画面が「更新あり」を出す) |
| `PUT /deploy/:deployId/configmap-mounts` | 全件置き換え → pending に保存 |
| `POST /deploy/:deployId/configmap-mounts/apply` | 適用(再起動を伴う。status は `applying`) |
| `POST /deploy/:deployId/configmap-mounts/discard` | pending を破棄 |

deploy の詳細取得にも、`configmapMounts` として、同じ内容を入れる。

PUT の検査(400 と 409):

- ConfigMap と key が、存在すること(別のプロジェクトの ConfigMap は、404)。
- path の書式は、今の deploy のファイルと同じ(`/` で始まる、英数字と `. _ - /`、200 文字まで、`..` と `//` と末尾の `/` は不可)。
- **path は、他のものと、まったく同じにならない**: ボリュームのマウント先、deploy の files、同じ ConfigMap のマウント同士。マウント先の**下**は、許す(今の files と同じ。実機で確認済み)。
- 上の逆向きの検査も要る: 既存の `PUT .../mounts` と `PUT .../files` は、**ConfigMap のマウントの path とも**、重ならないか確かめる。
- mode の書式。
- deploy の status が `applying`、`deleting`、起動中(`pending`)の間は 409(他の apply と同じ)。

## 6. 適用の処理(worker)

既存の `ApplyDeployFilesWorkflow` と同じ形の、新しい Workflow を作る(`ApplyDeployConfigMapMountsWorkflow`)。

1. 適用する版ごとに、k8s の ConfigMap `<configMapId>-r<revision>` を作る(すでにあれば、そのまま。`immutable: true`、`binaryData` と `data` を、`encoding` で振り分ける)。
2. Deployment を更新する。マウント 1 つにつき、`volumes` に ConfigMap のボリュームを 1 つ(`items: [{key, path: key, mode}]`)、`volumeMounts` に `subPath: key` のマウントを 1 つ。**mode は volume の `items` に付くので、マウントごとに、ボリュームを分ける**。
3. Pod の更新の方式は、ファイルやボリュームを持つ deploy と同じく **Recreate**。
4. 結果の確定: 成功なら `configmap_mounts` に、pending の内容を入れ、pending を null に戻し、status を戻す。使わなくなった版の k8s の ConfigMap を消す。失敗なら、Deployment を現在のマウントに戻し、status は `failed`(試して失敗した値は pending に残る)。
5. タイムアウトは 300 秒(他と同じ)。

config、mounts、files の適用でも、**現在の ConfigMap のマウントが、Deployment から落ちない**ようにする(files が、すでにそうしている。同じ方法で)。

## 7. 画面(frontend)

プロジェクトの画面に、**ConfigMap の一覧**(volume の一覧と同じ置き場所)。

- 作る: 名前を入れ、ファイルを足す。
- ファイルの足し方は 2 つ。
  - **アップロード**: ファイルを選ぶ(複数可)か、ドラッグアンドドロップ。ブラウザの `FileReader` で、バイト列を読む。**UTF-8 として読め、NUL 文字が無ければ `text`**、そうでなければ `base64`。ファイル名が、key になる(使えない文字は、エラーで知らせる)。
  - **その場で書く**: key と、内容を、テキストエリアで書く(`text` のみ)。
- 画面で、ファイルごとに、大きさを出す。上限を超えるものは、送る前に、止める。`text` のファイルだけ、中身を編集できる。`base64`(バイナリ)のファイルは、「バイナリ(N バイト)」と出し、**差し替え**と**ダウンロード**だけ。
- 保存すると、新しい版ができる。版の一覧と、2 つの版の差分(`text` のファイルだけ)は、後の段階。
- 使っている deploy の一覧を出す。削除の 409 のときも、その一覧を出す。

deploy の画面に、**「ConfigMap」のタブ**(今の「ファイル」「ボリューム」の隣)。

- ConfigMap と key を選び、マウント先の path と mode を入れる(**ConfigMap の全ファイルを、1 つのディレクトリに、まとめて追加する**ボタンも用意する。key ごとに、`<ディレクトリ>/<key>` の要素を作るだけ)。
- 「新しい版があります(3 → 5)」を出し、「最新にする」で、版を更新して pending にする。
- pending の表示と、適用、破棄は、他のタブと同じ(`pending-popover`)。

frontend のモックの API(`lib/mock-api.ts`)にも、同じ形を足す。

## 8. 権限、クォータ、イベント

- どの API も、プロジェクトの持ち主だけ(他のユーザーのものは 404。volume と同じ)。
- ConfigMap の数は、クォータの対象に入れる案(作成のトランザクションの中で、ユーザーの行をロックして数える。[auth.md](auth.md) §8)。`quota_defaults` に `configmaps` を足す。
- プロジェクトのイベント([project-events.md](project-events.md))に、ConfigMap の作成、更新、削除と、deploy への適用の結果を出す。
- プロジェクトの削除では、namespace の削除で、k8s の ConfigMap は消える。DB の行も消える(CASCADE)。

## 9. RBAC、セキュリティ

- worker の RBAC は、すでに `configmaps` の get / create / update / delete を許している(deploy のファイルのため)。**immutable な ConfigMap は、更新できない**ので、版ごとに作り、消すだけ(update は、使わない)。
- 内容は、**平文**で DB に保存し、k8s の ConfigMap にも、そのまま入る。パスワードや秘密鍵は、置かないよう、画面に注意書きを出す。Secret への対応は、別の設計([templates.md](templates.md) の未解決 1)。
- ファイル名と path は、検査した書式だけ。内容は、ログに出さない。
- 画面の `text` の表示は、エスケープして出す(内容をそのまま HTML にしない)。

## 10. 今の deploy の「ファイル」との関係

- `/deploy/:id/files`(deploy だけのファイル。テキスト)は、**そのまま残す**。テンプレートの段階 B・C が、この機能の上に作られており、変えると影響が大きいため。
- 内側の仕組み(ConfigMap の作り方、subPath のマウント)は、同じ。**ConfigMap の名前**は、deploy のファイルが `<deployId>`、共有のものが `<configMapId>-r<revision>`で、ぶつからない。
- 将来は、files を、「その deploy だけの、非公開の ConfigMap」として、この仕組みにまとめられる(§12 の 5)。今回はやらない。
- files にも、mode を足したいが、それは、この設計の後(§12 の 6)。

## 11. 作る順番

小さく分け、1 つずつ、PR にする。

1. **段階 1(実装済み): ConfigMap の資源**(マイグレーション、モデル、repository、`/projects/:id/configmaps` と `/configmaps/:id` の API、版、クォータ、テスト、仕様書)。k8s には、まだ触れない。
2. **段階 2(実装済み): deploy へのマウント**(deploy の列、`/deploy/:id/configmap-mounts`、検査、worker の Workflow と Activity、mode、他の適用での保持、path の重なりの検査、テスト、実機で確認)。
3. **段階 3(実装済み): 画面**(ConfigMap の一覧と編集、アップロード、deploy のタブ、モックの API)。

画面を先に作ると、API が固まらないので、API が先。

## 12. 確認したいこと

1. **「版を固定する」でよいか**(§3)。deploy が、勝手に新しい版にならない。代わりに、共有の ConfigMap を直しても、deploy に、自動では、届かない(画面に「更新あり」と出し、利用者が、適用する)。「直したら、使っている deploy が、全て自動で再起動」にしたければ、別の設計(deploy の status の扱いと、失敗したときの、全体の巻き戻しが、難しくなる)。
2. **mode は、マウントごと**(deploy が決める)で、よいか。ConfigMap のファイルごとに持たせる案もある(同じファイルが、どの deploy でも、同じ権限)。マウントごとの方が、柔軟で、k8s の作りにも合う。
3. **上限**(§4)の数字。ファイル 1 つ 256 KiB、合計 512 KiB。もっと小さくても、困らないか。
4. 一覧と取得で、**中身を返すか**。大きい(最大 700 KiB)ので、一覧は返さず、取得も、`?include=content` を付けたときだけにする案。
5. 今の deploy の files を、将来、この仕組みに**まとめるか**(§10)。
6. 今の deploy の files にも、**mode を足すか**。足すなら、`{"path", "content", "mode"}` で、既存の保存済みの値は、`0644` として扱う。
7. 画面のアップロードで、**ディレクトリごと**(中のファイルを、全て)アップロードできるようにするか(`webkitdirectory`。key は、ファイル名だけなので、サブディレクトリの同名ファイルが、ぶつかる)。今回は、ファイルの複数選択だけ、にする案。
8. **ConfigMap の key に、サブディレクトリを許すか**。k8s の ConfigMap のキーは、`/` を含められない。今の設計は、ファイル名だけ。階層が必要な設定(例: `conf.d/a.conf`)は、**マウントの path で作る**(key は `a.conf`、path は `/etc/app/conf.d/a.conf`)。

## 13. 実装メモ

### 段階 1(ConfigMap の資源)

- 実装: `migrations/app/000015_create_configmaps.up.sql`、`shared/model/configmap.go`、`shared/repository/configmap_repository.go`、`backend/src/{service,controller}/configmap_*.go`。プロジェクトの削除では、`DeleteRecord` が ConfigMap も消す(版は外部キーの CASCADE)。
- 数の上限(50)は、プロジェクトの行をロックして数える。アカウントごとのクォータ(`quota_defaults.configmaps`)は、まだ入れていない。
- ファイルは、key の昇順で保存する。中身が同じ(順番が違っても)置き換えは、版を増やさない。
- base64 は、標準の形(パディングあり)で、改行と空白は不可。

### 段階 2(deploy へのマウント)

- 実装: `migrations/app/000016_add_deploy_configmap_mounts.up.sql`(`deploys.configmap_mounts`、`pending_configmap_mounts`)、`shared/model/deploy_configmap_mounts.go`、`shared/repository/deploy_configmap_mounts_repository.go`、`backend/src/{service,controller}/deploy_configmap_mounts_*.go`、`shared/workflow/apply_deploy_configmap_mounts.go`、`worker/src/{workflow,activity}/apply_deploy_configmap_mounts_*.go`、`worker/src/cluster/config_map_revision_operator.go`。
- **版は、保存のときに固定する**(`resolveConfigMapMounts`)。指定が無ければ、すでにマウントしている版を保ち、無ければ最新。適用の開始のときにも、ConfigMap・版・key が残っているかを、改めて確かめる(409)。
- **ロック**: deploy の行 → ConfigMap の行(共有ロック)の順。ConfigMap の削除は、ConfigMap の行を更新用にロックして、使用中かを確かめるので、保存と同時には進まない。
- **k8s の ConfigMap**: `<configMapId>-r<revision>`、immutable、ラベル `launchs.io/configmap-revision=true`。テキストは `data`、バイナリは `binaryData`。deploy のファイルの ConfigMap(名前は deploy の ID)とは、別のもの(`ConfigMapRevisionOperator`)。
- **Pod のボリューム**: マウントごとに 1 つ(`cm0`、`cm1` …。path の順)。mode は、ボリュームの `items[].mode` に付くため。`subPath` で、読み取り専用でマウントする。更新方式は Recreate。
- **他の適用でも落ちない**: `newDeploymentDefinition` が、現在の `configmap_mounts` を入れる(config、mounts、files、scale、crash-loop、作成)。
- **後始末**: 成功したあと、Workflow が `CleanupConfigMapRevisions` を呼び、プロジェクトのどの deploy(現在と pending)も使っていない版の k8s の ConfigMap を消す。失敗しても、反映の結果には影響させない(次の反映のときに、また消す)。deploy を削除したあとの版は、次にそのプロジェクトで ConfigMap のマウントを反映したときに消える(プロジェクトの削除では、namespace ごと消える)。
- **path の重なり**: ConfigMap のマウントと、files、ボリュームのマウント先は、まったく同じ path にならない(その下は許す)。files と mounts の保存・適用の側からも、同じ検査をする。(ConfigMap のマウント + files)は、合計 40 個まで。
- **RBAC**: worker に、`configmaps` の list も要る(版の ConfigMap を、ラベルで探すため)。update は、版を更新しないので、使わない。
- **実機での確認(Talos。2026-11-18)**: nginx:1.27-alpine の deploy に、テキスト 2 つとバイナリ 1 つ(4096 バイト。NUL と 0xff を含む)の ConfigMap を、4 か所にマウントして確認した。
  - mode(0600、0444、0644、0755)が、実際のコンテナで、そのとおりに効く。バイナリは、`binaryData` として置かれ、md5 が元と一致する。UTF-8 の日本語、`run.sh` の実行も問題ない。マウントは読み取り専用。k8s の ConfigMap は immutable。
  - ConfigMap を編集(版 2)しても、deploy の Pod は版 1 のまま。`latestRevision` が 2 になる。版を明示した 2 つだけを版 2 にして適用すると、k8s に `-r2` ができ、版 1 は、使うマウントが残るので、消えない。
  - 起動できないマウント(`/proc/cmfail`)を足して適用すると、300 秒で `failed` になり、Deployment が元の 4 マウントに戻って、Pod が復活する。`mounts` は変わらず、`pending_mounts` は残る(破棄しても、status は、次の適用の成功まで `failed` のまま。他の pending と同じ)。
  - 全てのマウントを外して適用すると、版の ConfigMap が全て消え、ConfigMap を削除できる(使用中は 409)。プロジェクトを削除すると、namespace ごと消える。

### 段階 3(画面)

- 実装と使い方は、[frontend.md](frontend.md) の「ConfigMap の画面」。設計(§7)と違う点: ConfigMap の一覧は、構成図(キャンバス)のカードではなく、上のバーの「ConfigMap」タブにした(ファイルの編集に、広い場所が要るため)。
- **ブラウザでの確認(2026-11-18。実際のスタックと Talos)**: 作成、アップロード(テキストとバイナリ)、書いて追加、不正な key での保存の阻止、保存で版が増えること、編集、古い版の閲覧と、下書きへの置き換え。deploy のタブで、マウントの追加、不正な mode の阻止、ディレクトリへの一括追加、反映(Pod の中で、権限とバイナリの md5 を確認)、ConfigMap の編集 → 「新しい版あり」→「最新にする」→ 反映(版 4 が Pod に届き、版 3 の k8s ConfigMap が消える)、使用中の削除の阻止、「反映待ち」の表示と破棄。

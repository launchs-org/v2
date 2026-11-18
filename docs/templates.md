# テンプレート(設計)

**状態: 段階 A(deploy のファイル)、B(定義と検証)、E の一部(同梱 5 つ)、B2(ConfigMap)は実装済み。C(テンプレートから作る)、D(画面)は、これから。** 決定したことと、決めていないことを、分けて書く。実装は、下の「段階」の順に、小さな PR で進める。

利用者が、「PostgreSQL」「WordPress」のような、**よく使う構成**を、入力欄を埋めるだけで作れるようにする。構成は、YAML のファイルで定義する。初期設定の**設定ファイル**も、コンテナに渡せる。

## 決めたこと(2026-11-18)

| 項目 | 決定 | 理由 |
|---|---|---|
| 設定ファイルの渡し方 | **deploy に「ファイル」を持たせる**(新しい機能)。k8s の ConfigMap として、コンテナにマウントする | 初期設定だけでなく、あとから編集(pending → 適用)もできる。テンプレートは、この機能の上に乗る |
| テンプレートの定義の置き場 | **リポジトリの `templates/*.yaml`**。このシステム(backend)に同梱する | 内容を PR でレビューできる。DB も、管理画面も、要らない |
| 作れる人 | 管理者が、リポジトリに追加する。利用者は、選んで使うだけ | 安全(利用者が、任意の構成を、流し込まない) |
| 範囲 | **複数の部品**(deploy、volume、service、ingress、ファイル)を、1 つのテンプレートで | WordPress = web + DB(ボリューム付き)+ ingress、が書ける |
| 変数 | **ある**。YAML の中に `${名前}` と書き、画面に入力欄を出す。パスワードは、**自動生成**の型を用意する | 初心者が、弱いパスワードを使わない |
| 秘密の値 | **今の env と同じ、平文**(DB に保存、API で返す)。まずは、シンプルに | k8s Secret への分離は、別の課題(下の「未決」) |
| 作成後のつながり | **残さない**。作ったあとは、普通の deploy、volume として編集する。テンプレートを更新しても、既存のものは、変わらない | シンプルで、安全。追従は、必要になってから |

## 段階(PR の分け方)

| 段階 | 内容 | 前提 |
|---|---|---|
| **A. deploy のファイル** | deploy に、ファイル(パス + 内容)を持たせ、ConfigMap としてマウントする。API(`/deploy/:id/files`)、pending → 適用、画面のタブ | なし(単独で役に立つ) |
| **B. テンプレートの定義と検証** | `templates/*.yaml` の形(スキーマ)、読み込み、検証、変数の展開(純粋関数)。`GET /templates`、`GET /templates/:id` | なし |
| **C. テンプレートから作る** | `POST /projects/:projectId/templates/:id/apply`(変数を受けて、部品を、順に作る Workflow)。進み具合の確認 | A、B |
| **D. 画面** | ウィザードに「テンプレートから」を足す。入力欄、自動生成のパスワード、進み具合 | C |
| **B2. ConfigMap の部品**(実装済み) | テンプレートに、共有の ConfigMap([configmap.md](configmap.md))を足す。`kind: configmap` と、deploy の `configmaps`。テキストと、base64 のバイナリ、権限(mode)に対応する | B |
| **E. 同梱のテンプレート** | nginx、PostgreSQL、Redis、MariaDB、WordPress など、数個 | B |

A と B は、独立しているので、並行して作れる。

## テンプレートの YAML(案)

```yaml
# templates/postgres.yaml
apiVersion: launchs/v1
kind: Template
metadata:
  id: postgres              # URL とファイル名に使う。小文字の英数字とハイフン
  name: PostgreSQL
  description: データベース(PostgreSQL 16)。データは、ボリュームに保存されます。
  icon: database            # 画面の絵(決まった名前から選ぶ)
  category: database        # 絞り込み(database / web / cache / tool)

# 利用者が、入力する値。${名前} で、下の resources の中から使う。
variables:
  - name: database
    label: データベース名
    type: string
    default: app
    pattern: "^[a-z][a-z0-9_]{0,30}$"
  - name: user
    label: ユーザー名
    type: string
    default: app
  - name: password
    label: パスワード
    type: password          # password は、「自動で作る」を既定にする(入力もできる)
    generate: true          # 自動生成(英数字 24 文字)
  - name: storage
    label: ボリュームの大きさ(MB)
    type: integer
    default: 1024
    min: 256
    max: 5120

# 作る部品。上から順に作る。同じ名前(name)で、互いを参照する。
resources:
  - kind: volume
    name: data
    sizeMB: ${storage}

  - kind: deploy
    name: db
    image: postgres:16
    spec: small
    replicas: 1
    env:
      POSTGRES_DB: ${database}
      POSTGRES_USER: ${user}
      POSTGRES_PASSWORD: ${password}
      PGDATA: /var/lib/postgresql/data/pgdata
    mounts:
      - volume: data         # 上の volume の name
        path: /var/lib/postgresql/data
    files:                   # 設定ファイル(段階 A の機能)
      - path: /docker-entrypoint-initdb.d/init.sql
        content: |
          CREATE EXTENSION IF NOT EXISTS pgcrypto;
    service:
      ports:
        - name: postgres
          port: 5432
          protocol: tcp

  # ingress は、任意。HTTP の部品だけ(postgres は、無し)。
```

WordPress の例(2 つの deploy をつなぐ):

```yaml
resources:
  - kind: volume
    name: dbdata
    sizeMB: 1024
  - kind: deploy
    name: db
    image: mariadb:11
    env: { MARIADB_DATABASE: wp, MARIADB_USER: wp, MARIADB_PASSWORD: "${password}", MARIADB_RANDOM_ROOT_PASSWORD: "1" }
    mounts: [{ volume: dbdata, path: /var/lib/mysql }]
    service: { ports: [{ name: mysql, port: 3306, protocol: tcp }] }
  - kind: deploy
    name: web
    image: wordpress:6
    env:
      WORDPRESS_DB_HOST: ${service.db.dns}      # 他の部品の service の DNS 名
      WORDPRESS_DB_NAME: wp
      WORDPRESS_DB_USER: wp
      WORDPRESS_DB_PASSWORD: ${password}
    service: { ports: [{ name: http, port: 80, protocol: tcp }] }
  - kind: ingress
    rules:
      - path: /
        deploy: web          # この deploy の service
        port: 80
```

### ConfigMap の部品(`kind: configmap`)

共有の設定ファイルの組。**複数の deploy が、同じファイルを使える**。バイナリ(base64)も、ファイルごとの権限(mode)も扱える(deploy 自身の `files` は、1 つの deploy だけの、テキストのファイル)。

```yaml
resources:
  - kind: configmap
    name: conf                # 他の部品と同じ name の規則。volume、deploy とも、重複できない
    files:
      - key: nginx.conf       # ConfigMap の中のファイル名(`/` は使えない)
        content: |
          server { listen 80; server_name ${project.name}; }
      - key: logo.png
        encoding: base64      # バイナリ。省略は text
        content: iVBORw0KGgo...
  - kind: deploy
    name: web
    image: nginx:alpine
    spec: small
    configmaps:               # どのファイルを、どこに置くか(ファイル名は、変えられる)
      - configmap: conf
        key: nginx.conf
        path: /etc/nginx/conf.d/default.conf
        mode: "0644"          # 省略は 0644
```

- 保存する大きさは、1 つの configmap で 1 MiB まで、ファイルは 50 個まで([configmap.md](configmap.md) §4)。
- `text` の内容には、変数と `${project.name}` を書ける。`${service.X.dns}` は、**書けない**(configmap は、deploy より先に作るため)。`base64` の内容には、`${...}` を書けない(展開で、壊れるため)。
- 展開した**あと**の内容も、改めて検査する(変数の値で、1 MiB を超える、NUL 文字が入る、は断る)。
- deploy の `configmaps` の path は、その deploy の `files` と、`mounts` のマウント先と、**まったく同じにならない**(下は、よい)。`configmaps` と `files` を合わせて、40 個まで。
- 作るとき(段階 C)は、**版 1** の ConfigMap を作り、deploy は、その版を固定してマウントする。

### 変数の型

| type | 入力欄 | 検査 |
|---|---|---|
| `string` | 1 行の文字 | `pattern`、`minLength`、`maxLength` |
| `integer` | 数 | `min`、`max` |
| `password` | 伏せ字 + 「自動で作る」 | `generate: true` なら、既定は自動(英数字 24 文字。暗号論的な乱数)。長さは、`length` で変えられる |
| `choice` | 選択肢 | `options: [{value, label}]` |
| `boolean` | スイッチ | なし |

### 参照できる値(`${...}`)

| 書き方 | 意味 |
|---|---|
| `${変数名}` | 利用者が入れた値(自動生成を含む) |
| `${service.<deploy の name>.dns}` | その deploy の service の DNS 名(作成後に決まる。service が `active` になってから、展開する) |
| `${project.name}` | プロジェクトの名前 |

- 展開は、**文字列の置き換えだけ**(式や、条件分岐は、無い)。意図しない評価を、避けるため。
- 知らない `${...}` は、**定義の読み込みの時点で、エラー**にする(実行の途中で、気づかない)。

## 検証(読み込みのときと、実行の前)

- スキーマ: 必須の項目、型、`metadata.id` の形、変数名の重複、`resources` の `name` の重複。
- 参照: `mounts[].volume`、`ingress.rules[].deploy`、`${service.X.dns}` の X が、存在する。**循環**(A が B を参照し、B が A を参照)は、拒否。
- 値の範囲: deploy のスペック(`small`/`medium`/`large`)、ポート(1〜65535)、ファイルのパス(後述)。
- クォータ: 作る前に、テンプレート全体で、上限(deploy の数、volume の数と合計の大きさ)を超えないか、**まとめて**確かめる(途中で足りなくなって、半端に残さない)。
- 変数の値: `pattern`、`min`/`max` などを、サーバーで、必ず検査する(画面の検査は、補助)。

## 作る流れ(段階 C)

`POST /projects/:projectId/templates/:id/apply`(Body: `{ "values": { "database": "app", "password": "…" } }`)

1. テンプレートと変数を検査し、**クォータを、全体で確認**する(失敗なら、何も作らない。409 か 400)。
2. Temporal の Workflow を開始し、`applyId` を返す(非同期)。
3. Workflow は、`resources` を、**依存の順**(volume → deploy → service → ingress)に作る。deploy の作成、ファイル、マウント、service、ingress は、既存の仕組み(API と同じ処理)を使う。`${service.X.dns}` は、service が `active` になってから、展開する。
4. 進み具合を、`GET /projects/:projectId/template-applies/:applyId` で返す(部品ごとに、待機中/作成中/完了/失敗)。
5. **途中で失敗したら**: 作れたものは、**そのまま残す**(自動では消さない)。失敗した部品と理由を返す。利用者は、画面で、残りを手で直すか、消す。(自動のロールバックは、別の部品を、利用者の知らないうちに消すので、しない。)

## deploy のファイル(段階 A)

- モデル: deploy に `files`(`[{ path, content }]`、JSON の列)と、`pending_files` を持たせる。env、command、args、mounts と同じ「pending → 適用」。
- k8s: 適用のとき、deploy ごとに **ConfigMap**(名前は deploy の ID)を作り、`subPath` で、1 ファイルずつ、コンテナにマウントする(ディレクトリ全体を隠さない)。ファイルを変えて適用すると、Pod は再作成される(ConfigMap の更新だけでは、`subPath` のマウントは、更新されないため)。
- API: `GET /deploy/:id/files`、`PUT /deploy/:id/files`(全件置き換え → pending)、`POST .../files/apply`、`POST .../files/discard`。env、mounts と同じ形。
- 制限(案): 1 ファイルの内容は 64 KiB まで、1 deploy に 20 ファイルまで、合計 256 KiB まで(ConfigMap の上限は 1 MiB。余裕を持つ)。
- パスの規則: `/` で始まる。英数字と `. _ - /` だけ。`..` を含まない。200 文字まで。同じパスは、重複不可。**マウント(ボリューム)のパスと、重ならない**(同じ場所を、二重に覆わない)。
- 内容は、テキスト(UTF-8)だけ。バイナリは、扱わない。
- 秘密を含む設定ファイル(パスワード入りなど)も、**平文**で保存される(env と同じ)。これは、「未決」の 1 に書く。

## 画面(段階 D)

- 「+ 追加 → デプロイ」のウィザードの、最初の画面に、「**テンプレートから**」と「**イメージから**」の 2 択。
- テンプレートを選ぶと、カード(アイコン、名前、説明、カテゴリ)で一覧。選んだら、**変数の入力欄**(パスワードは、自動で入っていて、見せる/コピーできる)。「作成する」で、進み具合のチェックリスト(部品ごと)。
- 完了すると、構成図に、部品が、まとめて現れる。公開 URL のリンク。

## 同梱のテンプレート(段階 E、最初の案)

nginx(静的な公開)、PostgreSQL、MariaDB、Redis、WordPress(web + DB)、Node.js / Python の最小のアプリ(イメージを入れるだけ)。**各テンプレートは、実機(Talos)で、作成と起動を、確認してから、入れる。**

## セキュリティ

- テンプレートは、**リポジトリの管理下のファイルだけ**(利用者は、定義を、送れない)。
- 変数の値は、サーバーで検査し、テンプレートの定義の、決まった場所にだけ、**文字列として**入れる(YAML の構造に、影響させない。展開は、YAML を読み込んだ**あとの値**に、行う)。
- ファイルの内容に、`${...}` が含まれていても、利用者の入力に含まれるものは、**再展開しない**(展開は、1 回だけ)。
- 自動生成のパスワードは、`crypto/rand` で作る。ログには、出さない(`shared/logger` の、機密の属性)。

## 未決(決めていないこと)

1. **秘密の値の扱い**: 今は、平文(env と同じ)。パスワードを、k8s Secret に分け、画面では伏せ字にする設計は、別の課題。テンプレートが増えて、データベースのパスワードを扱うので、早めに検討したい。
2. **テンプレートの版**: ファイルを更新したとき、作成済みの deploy と、どう関係するか(今は、無関係)。
3. **設定ファイルの、バイナリや、大きなファイル**: 今は、扱わない(64 KiB のテキスト)。
4. **テンプレートの、公開範囲**: 今は、全員に同じ。ユーザーごとに、見せるものを変えるか。
5. **`${service.X.dns}` 以外の、部品間の参照**(ClusterIP、ポート番号、他のテンプレートの値)。
6. **途中の失敗の、自動のやり直し**: 今は、手で。
7. **テンプレートの、アップデートの通知**: 作成済みの deploy に、「新しい版があります」と出すか。

## 実装の前に、確認したこと(2026-11-18)

- **ConfigMap の `subPath` は、ボリュームと共存する**(Talos で確認)。同じ Pod で、ボリュームを `/data` に、ConfigMap の 1 ファイルを `/data/init.sql`(ボリュームの上)と `/etc/app/app.conf`(別の場所)にマウントして、読めた。ボリュームへの書き込みも、できた。→ 段階 A は、この方式で、作れる。
- **作成の処理は、既存の service 層を再利用できる**。deploy、service、volume、ingress の作成は、backend の `DeployService.Create`、`ServiceService.Create`、`VolumeService.Create`、`IngressService.Create` にあり、それぞれが、既存の Workflow を起動する。→ 段階 C は、これらを、**テンプレートの依存の順に呼ぶ**形で作れる(新しい Workflow は、「順に作り、service の `active` を待ち、DNS 名を、次の部品に渡す」ための、薄いものでよい)。
- 参照の仕方は、段階 A(ConfigMap)と、段階 C で、実装するときに、もう一度、確かめる。

## まだ確認していないこと

- 段階 B の、YAML の読み込みに使う、Go のライブラリ(`gopkg.in/yaml.v3` か、`sigs.k8s.io/yaml`)と、展開の実装(YAML を読んだ**あとの値**に、再帰で、置き換える)。
- 段階 C の、クォータの「まとめての確認」を、既存のクォータの仕組み(ユーザーの行をロックして、数える)に、どう、組み込むか。deploy を、1 つずつ作る既存の処理の前に、全体の数を、確認する、別のメソッドが要る。

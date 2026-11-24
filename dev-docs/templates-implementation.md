# テンプレート: 実装のメモ(段階 B)

設計は [templates.md](templates.md)。この文書は、段階 B(定義、読み込み、検証、展開、`GET /templates`、`GET /templates/:id`)の実装で、**決めた細部と、分かったこと**を書く。設計の決定は、変えていない。

## 構成

| 場所 | 役割 |
|---|---|
| `templates/<id>/` | 同梱のテンプレート(`nginx`、`redis`、`postgres`、`mariadb`、`code-server`)。ファイル名は、`metadata.id` と同じ |
| `templates/embed.go` | `//go:embed *.yaml` で、YAML をバイナリに埋め込む(`templates.Files`)。イメージに入れ忘れず、利用者が変えられない |
| `shared/template/` | 読み込み(`Parse`、`Load`)、検証(`Validate`)、値の検査と補い(`ResolveValues`、`ValidateValues`、`GeneratePassword`)、展開(`Expand`)。**データベースにも k8s にも触らない、純粋な処理** |
| `backend/src/service/template_service.go`、`backend/src/controller/template_controller.go` | 一覧と取得の API。`resources` は返さず、`summary` だけ |
| `backend/src/main.go` | 起動時に `template.Load` を呼ぶ。**1 つでも不正なら、backend は起動しない** |
| `backend/src/server/server.go` | `Dependencies.TemplateRegistry`。nil のとき(テスト)は、同梱のものを読み込む |

API の仕様は [endpoints/templates.md](endpoints/templates.md)。

## 使ったライブラリ

- `gopkg.in/yaml.v3`(`KnownFields(true)`)。**未知のフィールド(typo)は、エラー**。複数のドキュメントも、エラー。`go.mod` では、間接の依存だったものを、直接の依存にした。

## 決めた細部

- **変数の値は、全て、文字列**(`integer` の既定値も、YAML では `"1024"` のように、文字列で書く)。型の検査は、`validateValue` が、文字列を読んで行う。
- **`pattern` は、値の全体に一致させる**(`^(?:…)$` で包む)。一部だけの一致では、通さない。
- **`string` と `password` は、改行と NUL を、許さない**。環境変数や、設定ファイルの 1 行を、壊されないため。複数行の入力が要る変数が出たら、型を足す(今は無い)。
- **volume の `sizeMB` に変数を使うときは、`min` と `max` が必須**(`integer` の変数だけ。`min` は 1 以上)。展開の前に、範囲を保証するため。
- **`${...}` は、完全な参照だけ**。`sizeMB` では、`${storage}` の 1 つだけ(`"${storage}0"` は不可)。
- **展開は、YAML を構造として読んだあとの、文字列の値に対して、1 回だけ**。YAML のテキストへの、直接の文字列置換は、しない。変数の値に、改行、`:`、`"`、`#`、`${x}` があっても、構造は壊れず、再展開もされない(テストで確認)。
- **`ingress` の転送先は、tcp のポートだけ**(ingress は HTTP だけ)。`ingress` は、`name` を持たず、1 つだけ。`rules` が空の ingress は、拒否(ルールの無い ingress は、外から届かないため)。
- **自分自身の `${service.<自分>.dns}` は、拒否**(循環の、最小の形)。deploy 同士の循環は、深さ優先で検出し、経路を、エラーに出す。
- **`metadata.id` は、ファイル名と一致させる**。同じフォルダのファイル名は重複しないので、これだけで、`id` の重複も、起こらない(別に、重複を確かめるコードは、到達不能になるので、持たない)。
- **`Load` は、フォルダの直下の `*.yaml` だけ**。サブフォルダは、読まない。
- **`password` の自動生成**: 英数字 62 文字から、`crypto/rand` の `rand.Int` で、1 文字ずつ選ぶ(剰余の偏りを避ける)。既定 24 文字(`length` で、8〜128)。**API は、`generate: true` の `default` を、定義にあっても、返さない**。
- **service のポート名の規則**は、`dev-docs/endpoints/services.md` と同じ(小文字の英数字とハイフン、15 文字まで、英字を 1 つ以上、先頭と末尾と連続するハイフンは不可)。`model` に、検査の関数が無かったので、`template` パッケージの中に、同じ規則を書いた。**将来、`model` に寄せて、規則がずれないようにするとよい**。
- **ファイルの検査は、`model.ValidateFiles` を再利用**(マウント先と、まったく同じ path は、拒否。下は、許す)。マウントの検査も、`model.ValidateMounts` を再利用(テンプレートの中の volume の name を、仮の ID に置いて、渡している)。

## API の形(設計との違い)

- 一覧は、`{"templates": [...]}`(配列そのものではなく、他の一覧と同じ、オブジェクトで包む)。
- 変数は、`default`(自動生成の password は null)、`required`、`generate`、`options`(空でも配列)、`min`、`max`、`minLength`、`maxLength`、`pattern` を返す。`minLength` と `maxLength` は、設計の例には無かったが、定義にあるので、返す。
- 段階 C の、テンプレートから作る API は、まだ無い。

## 段階 C への申し送り

- `Expand` は、`${service.<deploy>.dns}` を、`ExpandContext.ServiceDNS` から引く。**service が `active` になって、DNS 名が決まってから**、その deploy を、展開する(deploy の作成は、依存の順に)。
- `ResolveValues` は、自動生成の password を、**呼ぶたびに、新しく作る**。段階 C は、API で受けた時点で 1 回だけ呼び、その結果(補った値)を、以後の処理に渡す(途中で、呼び直すと、別の password になる)。
- クォータの、まとめての確認は、`Summarize` の数(deploy、volume の数)と、`Expand` の volume の大きさの合計を使うと、作る前に、計算できる。

## 同梱のテンプレート(2026-11-18)

| id | イメージ | スペック | ボリューム | 公開 |
|---|---|---|---|---|
| `nginx` | `nginx:alpine` | small | なし(HTML は、ファイルで渡す) | ingress |
| `redis` | `redis:7` | **medium** | `/data`(AOF)。パスワードは、起動の引数 `--requirepass` で渡す | クラスターの中だけ |
| `postgres` | `postgres:16` | **medium** | `/var/lib/postgresql/data` | クラスターの中だけ |
| `mariadb` | `mariadb:11` | **medium** | `/var/lib/mysql`。root のパスワードは、ランダム(`MARIADB_RANDOM_ROOT_PASSWORD`)。利用者は、作った一般ユーザーでつなぐ | クラスターの中だけ |
| `code-server` | `codercom/code-server:latest` | **large** | `/home/coder/project`。ログインは `PASSWORD` | ingress |

- スペックは、「データベース系は medium、VS Code は large」の指示のとおり。
- データベースと Redis は、**外(インターネット)には、公開しない**(ingress は、HTTP だけで、TCP の DB は、通せないため)。同じプロジェクトの deploy から、service の DNS 名で、つなぐ。
- **実機(Talos)で、各イメージの起動と、中身を確認した**(テンプレートの適用は、段階 C なので、同じ内容を、手で作って確認した):
  - Redis: パスワードなしは `NOAUTH`、ありで、書き込みと読み出し。`appendonlydir` が、ボリュームに作られる。
  - PostgreSQL: 作った一般ユーザーで、接続できる。
  - MariaDB: 初回の初期化のあと、一般ユーザーで、接続できる(下の「実機の結果」)。
  - code-server: uid 1000 で、**ボリュームに書き込める**。`PASSWORD` で、ログインが有効。`/login` が 200。
- `codercom/code-server:latest` は、固定のタグではない(再作成のたびに、版が変わる)。版の固定は、段階 E で、検討する。

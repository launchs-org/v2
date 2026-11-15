# ログ収集(logcollector)の設計(レビュー待ち)

目的: deploy のコンテナの出力(標準出力・標準エラー)を、Pod が消えたあとも、保持期間の間、`GET /deploy/:deployId/logs` で見られるようにする。仕様は [docs/endpoints/deploys.md](endpoints/deploys.md) の「デプロイ ログ取得」、元の決定は [pre-implementation-checklist.md](pre-implementation-checklist.md) の I-3。

この文書は、**コードを書く前の設計**。レビューで決まったことと、残りの確認は、§10 にまとめた。

## 1. 決まっていること(既存の決定)

- ログ収集基盤(Loki など)は入れない。システム側の**収集コンテナ(`logcollector/`)**が、Pod のログを購読して **DB に保存**する。
- 収集コンテナが止まっても、別のものが引き継ぐ。切り替えで、欠落・重複を出さないよう、**Pod ごとの読み取り位置を DB に保存**する。
- 保持期間は **7 日**。Pod ごとの上限は **50MB**(超えたら、古い行から削除)。
- 終了・再作成された Pod のログも、保持期間内は、`podId` で取得できる。deploy を削除したら、ログも消える。
- API はポーリング(`since` / `nextSince`)。DB に書かれた分を返すので、Pod の出力から、少し遅れる。
- [watcher](watcher.md) の決まりのとおり、k8s の状態を読むのは、読み取り専用のコンテナだけ。API サーバーは、k8s の権限を持たない。

## 2. 構成

```
 k8s ──(pods/log の購読)──▶ logcollector(リーダー1つ)──▶ PostgreSQL(app DB)◀── backend(API。読むだけ)
                                  ▲                             ▲
                                  └── どの Pod を追うか: deploy_pods ─┘(watcher が書く)
```

- `logcollector/` は、他のコンテナと同じ形(`logcollector/dockerfile`、`logcollector/src/`)。Go モジュールは `launchs`。
- **ログを DB に書くのは、logcollector だけ**(watcher と同じ「書き手は1つ」の決まり)。backend は、読むだけ。
- **同時に1つだけ**動く。複数起動しても、PostgreSQL の advisory lock(`shared/leader`)で、リーダーになった1つだけが収集する。リーダーが止まる(プロセスの終了、DB の接続の切断)と、ロックが外れ、待っていた別の logcollector が引き継ぐ。
  - **Raft は使わない**(§10 で決定)。watcher と同じ理由(3台以上のクラスターと永続化が要り、重い。DB は、すでに必須)。
- k8s への権限は、**`pods/log` の `get` だけ**(全 namespace)。Pod の一覧は、k8s ではなく、DB の `deploy_pods` から知る(§4)。

## 3. データ(app DB。`migrations/app/` に足す)

| テーブル | 主な列 | 役割 |
|---|---|---|
| `deploy_log_streams` | `pod_uid`(PK)、`deploy_id`(FK、CASCADE)、`project_id`、`pod_name`、`started_at`、`finished_at`(NULL 可)、`resume_ns`、`resume_count`、`bytes` | Pod 1つの、収集の状態。**Pod が消えても残る**(保持期間の間)。`podId` の持ち主の確認(その deploy の Pod か)にも使う |
| `deploy_logs` | `id`(bigserial、PK)、`pod_uid`(FK、CASCADE)、`logged_ns`(行の時刻。unix ナノ秒)、`line` | ログの1行。`(pod_uid, id)` に索引 |

- `deploy_logs.id` は、全体で増える番号。**API の `since` / `nextSince` は、この `id`**(Pod の中で、増える。間が抜けてよい)。削除された行を指す `since` でも、「それより後」を返すだけなので、壊れない。
- `resume_ns` と `resume_count` は、**収集の読み取り位置**(§5)。ログの書き込みと同じトランザクションで更新する。
- `bytes` は、Pod の保存済みの合計(行の長さの合計)。書き込みのたびに足し、削除のたびに引く(§7)。
- 時刻を `timestamptz` にしないのは、PostgreSQL が マイクロ秒までで、k8s のログの時刻(ナノ秒)と、同じ時刻の行を区別できなくなるため。
- deploy を削除すると、CASCADE で、ストリームとログが消える。プロジェクトの削除も、同じ(deploy を消すため)。

## 4. どの Pod を追うか

- logcollector は、リーダーの間、**2 秒ごとに `deploy_pods` を読み**(`LOG_RECONCILE_INTERVAL`)、追う Pod の集合を決める。
  - `deploy_pods` にある Pod で、ストリームが無いもの → `deploy_log_streams` に行を作り、収集を始める。
  - 収集中の Pod が、`deploy_pods` から消えた(Pod が消えた)→ 残りを読み切って、`finished_at` を入れ、収集を止める。
  - 起動したとき(引き継いだときも同じ)は、まず、全件を同じ比較で整える。止まっていた間に増えた・消えた Pod が、反映される。
- 対象は、watcher が観測している Pod と同じ(`launchs-org/deploy-id` のラベルがある Pod)。それ以外の namespace(システムの Pod)のログは、集めない。
- **コンテナは `app` だけ**(`shared/k8s.AppContainerName`)。init コンテナや、サイドカーは、集めない(現状、作っていない)。
- watcher が止まっていると、新しい Pod を知るのが遅れる(watcher が戻れば、追いつく)。k8s の `pods` の watch を、logcollector にも持たせれば避けられるが、権限と処理が増えるので、しない(§10 で決定)。

## 5. 収集の流れ(Pod 1つにつき、1つの goroutine)

```
 1. 読み取り位置を DB から読む(resume_ns、resume_count)
 2. k8s に、pods/log を頼む: follow=true、timestamps=true、sinceTime=<resume_ns を秒に切り捨てた時刻>、container=app
 3. 1行ずつ読む。各行は「<RFC3339Nano の時刻> <本文>」。
    すでに保存した行を捨てる(下の「重複の除き方」)。残った行を、まとめて(最大 500 行、または 1 秒ごと)書く
 4. 書くときは、1つのトランザクションで: deploy_logs に追記 + deploy_log_streams の resume_ns / resume_count / bytes を更新
 5. ストリームが終わったら(コンテナの終了、接続の切断)、Pod がまだ deploy_pods にあれば、間隔を空けて、2 からやり直す
```

### 重複の除き方(欠落させず、重複もさせない)
- k8s の `sinceTime` は、**秒の精度**(RFC3339。`metav1.Time` の形式)で、その時刻と同じ秒の行から返ることがある。そのため、再接続のたびに、同じ行が、また届く。
- 読み取り位置は、「最後に保存した行の時刻(`resume_ns`)」と、「その時刻の行を、何行まで保存したか(`resume_count`)」。再接続の後に届く行のうち、**時刻が `resume_ns` より前の行は捨てる。時刻が `resume_ns` と同じ行は、最初の `resume_count` 行を捨てる。** それより後は、新しい行。
- 保存と位置の更新が、同じトランザクションなので、引き継いだ別の logcollector は、同じ位置から続けられる(**二重に保存しない。取りこぼさない**)。
- **要確認(実機)**: `sinceTime` が、同じ秒を含むか、含まないか、kubelet の挙動を、Talos の実機で確かめる。含まない場合、上の「捨てる」が、行を捨て過ぎないかも、あわせて確かめる(§11 の PR 4)。

### コンテナの再起動
- CrashLoopBackOff などで、コンテナが再起動すると、ストリームが切れる。logcollector が切れていた間に、**前のコンテナの最後の出力**が、`follow` では取れない。
- そこで、再接続のときに、watcher が書いた `deploy_pods.restart_count` が、前回より増えていれば、**先に `previous=true` で、前のコンテナのログの残りを1回だけ読む**(同じ重複の除き方)。そのあと、いまのコンテナを `follow` する。クラッシュの原因は、前のコンテナの最後の出力にあることが多い。
- 再起動が、収集の切断より速く、複数回あったときは、前のコンテナの分しか取れない(k8s が持つのは、直前の1つだけ)。

### 失敗したとき
- k8s への接続の失敗は、Pod ごとに、間隔を空けてやり直す(1 秒から、最大 30 秒まで)。Pod が消えている(404)なら、`finished_at` を入れて、止める。
- DB への書き込みの失敗は、位置を進めずに、同じ行を、やり直す。DB の接続が切れて、リーダーでなくなったら、全ての収集を止める。
- 収集の goroutine が、同時に動く数に上限を付ける(`LOG_MAX_STREAMS`、既定 500)。超えたら、警告を出して、新しい Pod の収集を始めない。

## 6. 行の扱い

| 項目 | 扱い |
|---|---|
| 1行の長さ | 16KiB まで(`LOG_MAX_LINE_BYTES`)。超えたら、切り詰めて、末尾に `…(truncated)` を付ける |
| NUL バイト | 取り除く(PostgreSQL の `text` に入れられないため) |
| 不正な UTF-8 | `U+FFFD` に置き換える |
| 空行 | そのまま保存する |
| 標準出力と標準エラー | 区別しない(k8s のログ API が、混ぜて返すため) |
| 書き込みの速さ | Pod ごとに 1MiB/秒まで(`LOG_INGEST_BYTES_PER_SEC`)。超えた分は捨て、1 秒に1行、`[logcollector] N 行を捨てました` を残す(§10 の A) |

## 7. 保存期間と上限(リーダーの logcollector が削除する)

始めに1回、そのあとは **1 分ごと**。削除に失敗しても止まらず、次の間隔でやり直す。

- **期間**: 行の時刻が 7 日より古いもの(`LOG_RETENTION`)を削除する。**終了して 7 日たったストリーム**(`finished_at` が 7 日前より古い)は、行ごと削除する。ストリームが無くなると、その `podId` は 404 になる。
- **Pod ごとの大きさ**: `bytes` が 50MiB(`LOG_MAX_BYTES_PER_POD`)を超えたストリームは、新しい行から数えて 50MiB を超えた分の、古い行を削除する。**上限は目安**(1 分の間に、書き込みの速さの上限の分だけ、超えることがある)。
- 古い行を削除しても、`since` は壊れない(§3)。

## 8. API(backend。読むだけ)

`GET /deploy/:deployId/logs?podId=...&since=...`(仕様は `deploys.md`。**ここに書く部分が、いまの仕様からの変更案**)。

- 持ち主の確認: deploy が、リクエストしたユーザーのものであること(他人・存在しない deploy は 404)。
- `podId`(`pod-` + Pod の UID)の `pod_uid` が、**その deploy のストリームにあること**。無ければ 404(その deploy に属したことが無い、または保持期間を過ぎて削除済み)。ストリームはあるが、行が全て削除されているときは、200 で空。
- 返すのは、`id > since` の行を、`id` の順に、**最大 1000 行、または 1MiB(どちらか先)**。`nextSince` は、返した最後の行の `id`。返す行が無ければ、`since` のまま(`since` を省略して、行が無ければ 0)。
  - **仕様の変更(決定)**: いまの仕様の「`since` を省略すると、先頭からの全ログ」は、**最大 1000 行ずつ**にする(全ログは、大きくなりうるため)。続きは、`nextSince` を `since` に渡す。返る行が空になったら、いまの末尾まで。
  - `since` が、末尾を超えるとき: 仕様のとおり、エラーにせず、`logs` は空文字、`nextSince` は、いまの末尾の `id`(その Pod の最大の `id`)。
- `logs` は、各行を `<JST の時刻> <本文>\n` にして、つなげた文字列(時刻は、k8s のログの時刻)。(§10 の C)
- 削除した deploy のログは、deploy と一緒に消える(CASCADE)。

### ログのある Pod の一覧(deploy の詳細に足す)

`GET /deploy/:deployId`(デプロイ詳細取得)のレスポンスに、**`logPods`**(ログのある Pod の一覧)を足す。終了した Pod の `podId` を、利用者が知るため(`pods` は、これまでどおり、稼働中の Pod だけ)。

```json
"logPods": [
  {"podId": "pod-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "running": true,  "startedAt": "2026-11-15T20:00:00+09:00", "endedAt": null},
  {"podId": "pod-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "running": false, "startedAt": "2026-11-15T19:00:00+09:00", "endedAt": "2026-11-15T19:55:00+09:00"}
]
```

- 対象: その deploy の `deploy_log_streams` のうち、**ログの行が1行以上残っているもの**(一覧にある `podId` は、必ず、ログが返る)。稼働中の Pod も含む(`pods` と重なる)。
- 並び: 新しい Pod(`startedAt`)が先。**最大 50 件**(それより古いものは、返さない。`podId` を知っていれば、ログは取れる)。
- `startedAt` は、Pod の作成時刻(`deploy_pods.pod_created_at`。収集の開始時にストリームへ写す)。`endedAt` は、収集が終わった時刻(`finished_at`)。稼働中は `null`。
- ログが無い Pod(起動前に落ちた、出力が無かった、保存期間で全て消えた)は、載せない。

## 9. 設定、権限、開発での試し方

| 環境変数 | 意味 | 既定 |
|---|---|---|
| `DATABASE_URL` | app データベース | 必須 |
| `KUBERNETES_ACCESS`、`KUBECONFIG` | k8s への接続 | 必須(`none` なら収集しない) |
| `LOG_RETENTION` | 行を残す期間 | `168h`(7日) |
| `LOG_MAX_BYTES_PER_POD` | Pod ごとの上限 | 50MiB |
| `LOG_MAX_LINE_BYTES` | 1行の長さの上限 | 16KiB |
| `LOG_MAX_STREAMS` | 同時に追う Pod の数の上限 | `500` |
| `LOG_INGEST_BYTES_PER_SEC` | Pod ごとの書き込みの速さの上限 | 1MiB |
| `LOG_RECONCILE_INTERVAL` | 追う Pod を決め直す間隔 | `2s` |
| `LOG_FLUSH_INTERVAL` | まとめて書く間隔 | `1s` |

- **本番の権限**(ServiceAccount): 全 namespace の `pods/log` の `get` だけ。DB は、app DB(`deploy_pods` を読み、`deploy_log_streams` / `deploy_logs` に書く)。
- **開発**: compose に `logcollector` を足す。既定は `KUBERNETES_ACCESS=none`(何も収集せず、待つだけ)。`task up:k8s`(`docker-compose.kubernetes.yaml`)で、worker・watcher と一緒に k8s につなぐ。`--scale logcollector=2` で、リーダーが1つだけになることを試せる(watcher と同じ)。
- **テスト**: k8s のログの取得は、interface(`LogSource`)の後ろに置き、テストでは偽物を使う(`client-go` の fake は、固定の文字列しか返さない)。重複の除き方、再接続、`previous`、引き継ぎ、保存期間は、偽物と、使い捨ての DB で確かめる。実際の `sinceTime` の挙動と、kubelet のログのローテーションは、実機(Talos)で確かめる。

## 10. レビューの結果と、残りの確認

### 決まったこと
1. **リーダー選出は、Raft ではなく advisory lock**(I-3 の「Raft は採用」を、この点で変更する。watcher と同じ理由)。
2. **追う Pod は、DB の `deploy_pods`(watcher が書く)から知る。**
3. **`since` は行の `id`。省略時は「最大 1000 行ずつ」**(`deploys.md` の仕様を、この形に変える。§8)。
4. **終了した Pod の `podId` は、deploy の詳細に `logPods`(ログのある Pod の一覧。終了した Pod も含む)を足して、返す**(§8)。

### 残りの確認(勧める案のまま進める。変えたいものがあれば、言ってください)
- **A. 書き込みの速さの上限**(Pod ごとに 1MiB/秒、超えた分は捨てて、1 秒に1行、捨てた数を残す)を入れる。入れないと、大量に出力する Pod が、DB の書き込みを占有する。
- **B. Pod ごとの 50MB は、目安**(最大で 1 分、超えることがある)。厳密にするなら、書き込みのたびに削除することになり、重くなる。
- **C. `logs` の各行に、時刻(JST)を付ける**(仕様の例のとおり)。
- **D. `app` コンテナだけ集める**(init コンテナ、サイドカーは、集めない)。
- **E. 保存期間は、行の時刻で数える**(終了して 7 日たったストリームは、行ごと削除)。DB に入った時刻で数えると、収集が遅れたときに、古い行がすぐ消える。

## 11. 実装の段階(PR の分け方の案)

1. **この設計の文書**。
2. **logcollector の土台**: テーブル(`deploy_log_streams`、`deploy_logs`)、リーダー選出、追う Pod の決め直し、収集(`LogSource` の interface と、k8s の実装)、重複の除き方、書き込み、保存期間の削除。偽物の `LogSource` と、使い捨ての DB でテスト。compose。
3. **API**: `GET /deploy/:deployId/logs`(backend)と、`GET /deploy/:deployId` の `logPods`、仕様書(`deploys.md`、Issue 002・009)の更新、`call-flow.md`。
4. **実機での確認**(Talos): `sinceTime` の挙動(同じ秒の行の重複と、取りこぼし)、コンテナの再起動(`previous`)、リーダーの切り替え(2つ起動して、片方を止める)、ログのローテーション、書き込みの速さの上限。本番の権限(`pods/log` の ClusterRole)。

2 と 3 を、1つにまとめることもできる(大きくなるので、分ける案)。

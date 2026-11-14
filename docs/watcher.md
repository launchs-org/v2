# watcher(k8s の実際の状態を DB に反映するコンテナ)

k8s の Pod と Deployment の変化を受け取り、**実際の状態**を DB に書く。ワーカーと API は、k8s に問い合わせず、DB を読む。

## 決まり

1. **実際の k8s の状態(観測値)を DB に書くのは、watcher だけ。** バックエンドとワーカーは、観測値のテーブルに書かない。
2. watcher は、**同時に1つだけ**動く。複数起動しても、PostgreSQL の advisory lock(`shared/leader`)で、リーダーになった1つだけが観測する。リーダーが止まる(プロセスの終了、DB の接続の切断)と、ロックが外れ、待っていた別の watcher が引き継ぐ。Raft は使わない(3台以上のクラスターと永続化が要るため、重い)。
3. **k8s が正で、DB は k8s の写し。** 起動したとき(引き継いだときも同じ)は、先に k8s の全件を読み込み、**DB を k8s の値で上書き**する(DB の古い値は使わない)。DB にあって k8s に無いものは削除する。そのあと、イベントの受け取り(watch)に入る。
4. watcher は、k8s を**読み取るだけ**。必要な権限は、全 namespace の `pods` と `deployments` の `get` / `list` / `watch` だけ(本番の ServiceAccount は、この ClusterRole にする)。

## 観測するもの

`launchs-org/deploy-id` のラベルがある Pod と Deployment だけ(ラベルは `shared/k8s/labels.go`)。

| テーブル | 中身 | 書き方 |
|---|---|---|
| `deploy_observations` | Deployment の generation / observedGeneration、レプリカ数(総数、最新の設定、Ready、使える) | deploy 1つに1行。上書き |
| `deploy_pods` | Pod の現在の状態(status、phase、Ready、再起動の回数、待っている理由、最後の終了の理由と終了コード、ノード、時刻) | Pod 1つに1行。Pod が消えると削除 |
| `deploy_pod_events` | Pod の状態変化のログ | 追記だけ |

どれも、deploy の行を消すと、一緒に消える(`ON DELETE CASCADE`)。

## Pod の状態変化のログ(`deploy_pod_events`)

DB に保存してある前の状態と、観測した今の状態の差から作る(`repository.DiffPodEvents`)。引き継いだときも、同じ比較で、止まっていた間の変化が残る。

| type | いつ |
|---|---|
| `created` | Pod が初めて観測された |
| `ready` / `not_ready` | Ready になった / なくなった |
| `restarted` | コンテナの再起動の回数が増えた(`reason` に OOMKilled / Error など、`message` に回数と終了コード) |
| `waiting` | 異常な理由(CrashLoopBackOff、ImagePullBackOff、Unschedulable など)で待ち始めた。ContainerCreating などの正常な待ちは記録しない |
| `terminating` | 終了の処理に入った |
| `deleted` | Pod が消えた |

CrashLoopBackOff の間は、再起動のたびに、`waiting`、`ready`、`restarted`、`not_ready` が繰り返し記録される。

## 心拍(watcher が生きているかの印)

`watcher_heartbeats`(1行だけ)。リーダーの watcher が、全件の反映を始めたあと、10 秒ごとに、DB の時計(`now()`)で書く(`model.WatcherHeartbeatInterval`)。

安定している deploy の観測値(`observed_at`)は、イベントがない限り更新されない。そのため、「観測値が新しいか」では、watcher が生きているかを判断できない。ワーカーは、**最後の心拍からの経過が 30 秒未満(`WatcherHeartbeatStaleAfter`)のときだけ**、観測値を信用する。比べるのも DB の時計なので、サーバーごとの時計のずれは関係しない。

## 観測値の使い手

| 使い手 | 何に使うか |
|---|---|
| API(`GET /deploy/:deployId`) | `pods` の一覧(`deploy_pods`) |
| ワーカー(`WaitDeploymentReady`) | 起動待ち(`deploy_observations` と心拍)。心拍が古い、または無いときは、k8s に直接問い合わせる |

### 起動待ち(ワーカー)
更新(作成)の Activity が、Deployment の設定の世代(generation)を返し、待機に渡す。次を全て満たしたときに Ready とする。
- 観測値の generation が、渡された世代以上で、`observed_generation` が追いついている(更新の直後に、古い設定の「Ready」を信用しないため)
- Pod の総数、最新の設定の数、準備完了、使える数が、全て、希望するレプリカ数と同じ

## クラッシュし続ける deploy を止める(上限)

k8s は、クラッシュしたコンテナを、間隔を最大 5 分まで延ばしながら、永久に再起動し続ける。`active` のまま壊れ続けないよう、**異常な状態が上限を超えたら、レプリカを 0 にして止め、`failed` にする**。

### 「安定して動いている」の定義
Pod が **Ready のままで、コンテナが 5 分以上動き続けている**(`model.PodStableAfter`)こと。クラッシュを繰り返す Pod は、一瞬 Ready になることがあるので、Ready になっただけでは回復とみなさない。

watcher は、Pod の保存のたびに、`deploy_pods.unhealthy_since`(安定して動いている状態でなくなった時刻)を更新する(`repository.NextUnhealthySince`)。安定すれば NULL、不健全が続く間は同じ時刻、安定して動いていたあとで崩れたら、その時刻から数え直す。

### 止める条件(`ListCrashLoopCandidates`。DB の時計で判定)
次を **全て** 満たす deploy を、1 分ごとに探す(`crashloop.Monitor`)。
- `status` が `active` で、希望するレプリカ数が 1 以上(作成中・反映中・削除中・すでに止まっているものには、触らない。作成と反映には、300 秒の期限がある)
- 終了処理中でない Pod の中に、**安定して動いている Pod が 1 つも無い**(3 台のうち 1 台だけ不調で、他が正常なら、止めない)
- Pod のどれかが、**安定して動いていない状態が 10 分以上**続いている(`CRASH_LOOP_UNHEALTHY_FOR`)

**猶予を 10 分にしている理由**: CrashLoopBackOff は、最初の数回のクラッシュ(約 1 分)で、もう出る。出た瞬間に止めると、依存先(DB など)が短く止まっただけで、全部の deploy が `failed` になり、利用者が手で戻すことになる。k8s の再起動の間隔は 10 秒から最大 5 分まで延びる(10 分で、およそ 6〜7 回の再起動)ので、短い障害は乗り越え、本当に壊れているものだけを止める。

**再起動の回数では止めない**: `restart_count` は累積の値で、何日も安定して動いてきた Pod が、一度クラッシュしただけで止まってしまう。また、再起動の間隔が延びるため、10 回に達するのは約 20 分後で、時間の条件のほうが、いつも先に満たされる。

### 止める処理(`StopCrashLoopWorkflow`。ワーカーが実行)
```
watcher の Monitor(リーダーだけ) → Temporal に Workflow の開始を頼む
  1. BeginCrashLoopStop   active の deploy を applying にし、`crash_loop_limit` のイベント(理由と、再起動の回数・続いた時間)を記録
                           active でなければ(別の処理が進んでいる)何もせずに終わる(重ねて頼んでも安全)
  2. ScaleToZero           k8s の Deployment のレプリカを 0 にする(コンテナの再起動が止まる)。DB の replicas は変えない
  3. FinishCrashLoopStop   deploy を failed にする(2 に失敗しても、applying のまま残さない)
```
止まったあと、`replicas`(希望する数)はそのまま。利用者は、**同じ数でスケールし直す**(`failed` の deploy は、同じ数でもスケールできる)か、設定を直して適用して、やり直せる。

## イベントの保存期間(上限)

`deploy_pod_events` は追記だけなので、上限を超えたものを、リーダーの watcher が削除する(`retention.Pruner`)。始めに1回、そのあとは1時間ごと。

- **期間**: 30 日より古いもの(`retention.PodEventRetention`)。
- **件数**: 1つの deploy につき、新しい順に 1000 件まで(`retention.MaxPodEventsPerDeploy`)。CrashLoopBackOff の間は、再起動のたびに数件ずつ増えるので、期間だけでは足りない。
- deploy の行を消すと、イベントは CASCADE で一緒に消える。
- 削除に失敗しても止まらず、次の間隔でやり直す。

## 処理の流れ

```
watcher/src/main.go
  └─ leader.Elector.Run            advisory lock を取れたものだけが、次へ進む(取れなければ待つ)
       └─ observer.Observer.Run
            1. 準備       informer(k8s の写し)と作業キューを作る
                          対象: deploy-id のラベルがある Pod と Deployment(全 namespace)
            2. 全件の読み込み   いまの k8s の全件を読み込む。読み込んだものは、「追加」のイベントとしてキューに入る
            3. 後片付け   DB にあって k8s に無い Pod・観測値を削除する(止まっていた間に消えたもの)
            4. 反映       キューのものを順に DB へ反映する(DB を k8s の値で上書き)。以降の変化も同じ道
                          (失敗したものは、間隔を空けてやり直す。同じものは1つにまとまる)
```

状態の上書きに、DB の前の値は使わない。前の値を使うのは、**イベントのログを作るときだけ**(前の状態との差から、ready や restarted を決める)。

- 状態の組み立ては `observer.BuildPodSnapshot` / `BuildDeploymentSnapshot`(k8s の型から DB の行を作る、副作用の無い関数)。
- DB の読み書きは `repository.DeployObservationRepository`。Pod は、行をロックして前の状態を読み、イベントを同じトランザクションで追記する。
- deploy の行がまだ無い(作成の途中で、k8s に先に現れた)ときは、保存せず、エラーにもしない。次のイベントで保存される。

## 設定(環境変数)

| 環境変数 | 意味 | 既定 |
|---|---|---|
| `DATABASE_URL` | app データベース | 必須 |
| `KUBERNETES_ACCESS`、`KUBECONFIG` | k8s への接続 | 必須(`none` なら観測しない) |
| `TEMPORAL_ADDRESS`、`TEMPORAL_NAMESPACE` | 止める Workflow の開始に使う | `temporal:7233`、`default` |
| `CRASH_LOOP_UNHEALTHY_FOR` | 安定していない状態が、これだけ続いたら止める | `10m` |

`CRASH_LOOP_UNHEALTHY_FOR` は、安定の判定(5 分)より長くする(短いと、起動したばかりの正常な Pod を止めてしまう)。0 以下の値は、全ての deploy を止めてしまうので、起動時に拒否する。

## 開発で試すとき

- `KUBERNETES_ACCESS=none`(既定)では、何も観測せず、待つだけ。`task up:k8s` で、ワーカーと一緒に、k8s につながる。
- `docker compose up --scale watcher=2` で、リーダーが1つだけになることを試せる。ただし、2つのコンテナが、同じ `watcher/tmp/watcher`(air のビルド結果)を共有するため、片方がビルドすると、もう片方も再起動する。開発の都合による。

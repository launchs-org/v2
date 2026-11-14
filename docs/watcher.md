# watcher(k8s の実際の状態を DB に反映するコンテナ)

k8s の Pod と Deployment の変化を受け取り、**実際の状態**を DB に書く。ワーカーと API は、k8s に問い合わせず、DB を読む。

## 決まり

1. **実際の k8s の状態(観測値)を DB に書くのは、watcher だけ。** バックエンドとワーカーは、観測値のテーブルに書かない。
2. watcher は、**同時に1つだけ**動く。複数起動しても、PostgreSQL の advisory lock(`shared/leader`)で、リーダーになった1つだけが観測する。リーダーが止まる(プロセスの終了、DB の接続の切断)と、ロックが外れ、待っていた別の watcher が引き継ぐ。Raft は使わない(3台以上のクラスターと永続化が要るため、重い)。
3. 起動したとき(引き継いだときも同じ)は、**先に全件を読み込み、DB との差分を反映**してから、イベントの受け取り(watch)に入る。止まっていた間の変化は、この差分で拾う。
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

## 処理の流れ

```
watcher/src/main.go
  └─ leader.Elector.Run            advisory lock を取れたものだけが、次へ進む(取れなければ待つ)
       └─ observer.Observer.Run
            1. informer を始める(deploy-id のラベルがある Pod と Deployment を、全 namespace で)
            2. 全件の読み込みを待つ
            3. DB にあって k8s に無い Pod・観測値を削除する(引き継ぎの片付け)
            4. イベントを、作業キューに入れて、順に DB へ反映する
                 (失敗したものは、間隔を空けてやり直す。同じものは1つにまとまる)
```

- 状態の組み立ては `observer.BuildPodSnapshot` / `BuildDeploymentSnapshot`(k8s の型から DB の行を作る、副作用の無い関数)。
- DB の読み書きは `repository.DeployObservationRepository`。Pod は、行をロックして前の状態を読み、イベントを同じトランザクションで追記する。
- deploy の行がまだ無い(作成の途中で、k8s に先に現れた)ときは、保存せず、エラーにもしない。次のイベントで保存される。

## 開発で試すとき

- `KUBERNETES_ACCESS=none`(既定)では、何も観測せず、待つだけ。`task up:k8s` で、ワーカーと一緒に、k8s につながる。
- `docker compose up --scale watcher=2` で、リーダーが1つだけになることを試せる。ただし、2つのコンテナが、同じ `watcher/tmp/watcher`(air のビルド結果)を共有するため、片方がビルドすると、もう片方も再起動する。開発の都合による。

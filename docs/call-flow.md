# コードの呼び出しの流れ

`New〇〇` が多くて流れが追いにくいときに読む文書。「どこで何が組み立てられ、リクエストがどう流れるか」を、プロジェクトの削除(`DELETE /projects/:projectId`)を例に説明する。

## 1. まず覚えること

- **`New〇〇` は、部品を作るだけ**。依存するもの(DB、他の部品)を引数で受け取り、構造体を返す。中で通信や処理はしない(`NewProjectService(repo, starter)` は、保存先と開始役を持った `ProjectService` を返すだけ)。
- **部品を組み立てる場所は、2か所だけ**。ここ以外で `New〇〇` を呼ばない(テストは除く)。
  - `backend/src/main.go` と `backend/src/server/route_registration.go`(バックエンド)
  - `worker/src/main.go`(ワーカー)
- **依存の向きは一方向**: `controller → service → repository → DB`。逆向きには呼ばない。
- **interface は「使う側」の場所にある**。`service` が欲しい保存先の形(`ProjectStore`)を `service` が定義し、`repository.ProjectRepository` がそれを満たす。だから、`service` は `repository` を import しない(テストでは fake を渡す)。

## 2. 起動時の組み立て

### バックエンド(`backend/src/main.go` の `run`)

```
main()
 └─ run()
     ├─ config.LoadBackendConfig(...)                  環境変数 → BackendConfig
     ├─ logger.New(...)                                ロガー
     ├─ database.Open(DATABASE_URL)                    DB の接続(*gorm.DB)
     ├─ client.Dial(TEMPORAL_ADDRESS)                  Temporal のクライアント
     ├─ service.NewTemporalProjectDeletionStarter(temporalClient)
     │                                                 「削除の Workflow を開始する役」
     ├─ server.New(logger, Dependencies{DB, DevUserID, 開始役})
     │   ├─ ミドルウェアを登録(リクエスト ID、アクセスログ、Recover、エラーハンドラー)
     │   └─ registerRoutes(...)                        ← ここで controller 以下を組み立てる
     │       ├─ repository.NewProjectRepository(DB)
     │       ├─ service.NewProjectService(repository, 開始役)
     │       ├─ controller.NewProjectController(service)
     │       └─ apiGroup.DELETE("/projects/:projectId", projectController.Delete) など
     └─ サーバーを起動
```

`server.Dependencies` は、「`main.go` が作ったもの(DB、開始役)を、ルートの組み立てに渡す入れ物」。

### ワーカー(`worker/src/main.go` の `run`)

```
main()
 └─ run()
     ├─ config.LoadWorkerConfig(...)                   環境変数 → WorkerConfig(k8s の設定を含む)
     ├─ database.Open / client.Dial                    DB と Temporal
     ├─ worker.New(temporalClient, "general")          Task Queue "general" を処理するワーカー
     ├─ RegisterWorkflow(DeleteProjectWorkflow)        Workflow を登録
     ├─ chooseClusterOperators(設定)                    ← 「どの実装を使うか」を選ぶ(下の 3 を参照)
     ├─ activity.NewProjectDeletionActivities(namespace を消す役, repository, logger)
     └─ RegisterActivity(...) × 3                      Activity を名前で登録 → Run()
```

## 3. 「選ぶ」処理: `chooseClusterOperators`(以前の factory)

k8s を操作する役(namespace を消す `activity.NamespaceDeleter`、namespace と Deployment を作る `activity.DeploymentOperator`)には、それぞれ実装が2つある。どちらを使うかは、環境変数 `KUBERNETES_ACCESS` で決まる。

```
chooseClusterOperators(設定, RUNTIME_CLASS)
 └─ k8s.NewClientset(設定)                              shared/k8s(他の部品でも使える共通部品)
     ├─ none の場合 → ErrNotConnected を返す
     │     → LoggingNamespaceDeleter / LoggingDeploymentOperator(何も操作せず、警告のログだけ。開発用)
     └─ kubeconfig / in-cluster の場合 → k8s のクライアント(1つだけ作って、全ての役で共有)
           → KubernetesNamespaceDeleter / KubernetesDeploymentOperator(client-go で本当に操作する)
```

「実装を選ぶ関数」は、ここ1か所だけ。k8s のクライアントの作り方そのものは `shared/k8s` にあり、別の処理が k8s につなぐときも、同じ `k8s.NewClientset` を使う。

## 4. リクエストの流れ: `DELETE /api/v1/projects/:projectId`

```
curl ──HTTPS──▶ nginx ──HTTP──▶ backend (Echo)
  ① ミドルウェア: リクエスト ID → アクセスログ → Recover
  ② 認証ミドルウェア(access token を JWKS の公開鍵で検証する。shared/jwtverify)が、ユーザー ID をコンテキストに入れる
  ③ ProjectController.Delete
       ユーザー ID と projectId を取り出して、service に渡す
  ④ ProjectService.Delete                             ビジネスルール
       ├─ projectStore.MarkDeleting(...)              repository: 行をロックして status を deleting に
       │    (無い/他人のもの→404、すでに deleting→409。delete_failed からはやり直し)
       ├─ projectDeletionStarter.StartProjectDeletion  Temporal に Workflow の開始を頼む(完了は待たない)
       └─ 失敗したら projectStore.RevertDeleting      受け付ける前の状態に戻す
  ⑤ すぐ 200 {} を返す
```

ここまでがバックエンドの仕事。以降は、ワーカーが非同期に行う。

```
Temporal サーバー ── Task Queue "general" ──▶ worker
  DeleteProjectWorkflow(順序を決めるだけ。外部とのやり取りはしない)
   ├─ Activity: DeleteProjectNamespace   k8s の namespace を削除し、完全に消えるまで待つ
   │     (期限 15 分までリトライ)
   ├─ Activity: DeleteProjectRecords     DB の行を物理削除 → 以後、GET は 404
   └─ どれかが期限までに終わらなければ
        Activity: MarkProjectDeleteFailed  status を delete_failed にして Workflow を失敗で終える
        (利用者は、もう一度 DELETE を呼んでやり直せる)
```

## 5. もう1つの例: deploy の作成(`POST /deploy`)

プロジェクトの削除と同じ形(バックエンドが受け付けて、ワーカーが非同期に実行する)。違いは、「成功すると status が active になる」ことと、namespace を**作る**側であること。

```
curl → ... → DeployController.Create(入力の検証: projectId、imageRef、spec)
  → DeployService.Create
      ├─ FindSpec            スペック(small など)がマスターデータにあるか
      ├─ DeployRepository.Create   プロジェクトの行を共有ロックして、active か確かめて、deploy を pending で保存
      │                            (削除中のプロジェクトには作れない → 409)
      ├─ Temporal に CreateDeployWorkflow の開始を頼む
      └─ 開始できなければ、保存を取り消す(起動されない deploy を残さない)
  → すぐ 200 {deployId} を返す(この時点の status は pending)
        ↓ ここから worker が非同期に実行
  CreateDeployWorkflow
   ├─ Activity: EnsureProjectNamespace  プロジェクトの namespace を、無ければ作り、**隔離の NetworkPolicy を適用する**([network-isolation.md](network-isolation.md))
   ├─ Activity: ApplyDeployment         DB の deploy とスペックから、k8s の Deployment を作る
   ├─ Activity: WaitDeploymentReady     Ready になるまで待つ(最長 300 秒。ワーカーが入れ替わっても引き継ぐ)
   └─ Activity: MarkDeployResult        成功なら active、失敗(300 秒で Ready にならない等)なら failed
```

k8s を操作する部品は、`activity.DeploymentOperator`(interface)で、実装は2つ。`KUBERNETES_ACCESS` で、`chooseClusterOperators`(`worker/src/main.go`)が選ぶ。

- `cluster.KubernetesDeploymentOperator`: client-go で、本当に操作する
- `activity.LoggingDeploymentOperator`: 何も操作せず、ログだけを出し、常に Ready と答える(`none`。開発用)

## 5.1 起動設定の反映: `POST /deploy/:deployId/config/apply`

`PUT /config` と `discard` は、DB の `pending_*` を書き換えるだけで、Workflow は使わない(`DeployRepository.SavePendingConfig` / `DiscardPendingConfig`。行を `FOR UPDATE` でロックして、状態を確かめる)。コンテナへの反映だけが、非同期になる。

```
curl → ... → DeployController.ApplyConfig
  → DeployService.ApplyConfig
      ├─ DeployRepository.BeginConfigApply   行をロックして、反映できる状態か確かめ、status を applying にする
      │     (pending が無い・applying・起動中・削除の途中なら 409。元の status を覚えておく)
      ├─ Temporal に ApplyDeployConfigWorkflow の開始を頼む
      └─ 開始できなければ、status を元に戻す(applying のまま残さない)
  → すぐ 200 {} を返す
        ↓ ここから worker が非同期に実行
  ApplyDeployConfigWorkflow
   ├─ Activity: ApplyPendingConfig   pending の値で Deployment を更新する(k8s がコンテナを入れ替える)
   ├─ Activity: WaitDeploymentReady  Ready になるまで待つ(作成と同じ Activity。最長 300 秒)
   ├─ (失敗したときだけ) Activity: RollbackDeploymentConfig  Deployment を、現在の正式な値に戻す
   └─ Activity: CommitConfigResult   結果が決まってから確定する。
                                      成功: 正式な値を pending の内容にして pending を空にし、active(replicas 0 なら scaled_zero)
                                      失敗: 正式な値と pending は変えず、failed にする
```

**実際の反映を先に試み、結果が確定してから** DB の正式な値を更新する。正式な値(env など)は常に「いま動いている値」で、失敗した値は pending に残る。PUT した内容が現在の値と同じなら、pending にならない(同じ設定の再適用はできない)。

## 5.2 スケーリング: `POST /deploy/:deployId/scale`

`config/apply` と同じ形。違いは、DB に**新しい replicas を先に保存**すること(replicas は「希望する数」なので、失敗しても戻さない)と、k8s を更新するときに、pending ではなく**現在の値**を使うこと。

```
DeployService.Scale
  ├─ DeployRepository.BeginScale   行をロックして、replicas を新しい値にし、status を applying にする
  │     (今と同じ数で failed でもなければ、何もせず 200。反映中・起動中・削除の途中なら 409)
  ├─ Temporal に ScaleDeployWorkflow の開始を頼む(開始できなければ、replicas と status を元に戻す)
  └─ すぐ 200 {deployId, replicas}
        ↓ worker が非同期に実行
  ScaleDeployWorkflow
   ├─ Activity: ApplyScale          現在の値で Deployment を更新する(replicas が変わる)
   ├─ Activity: WaitDeploymentReady  作成と同じ Activity
   └─ Activity: MarkScaleResult     成功なら active(replicas 0 なら scaled_zero)、失敗なら failed
```

## 5.3 deploy の削除: `DELETE /deploy/:deployId`

プロジェクトの削除(4章)と同じ形。違いは、消すのが namespace ではなく **Deployment 1つ**であることと、止まっている deploy(`delete_failed`)のやり直しができること。

```
DeployService.Delete
  ├─ DeployRepository.MarkDeleting   行をロックして、status を deleting にする(元の status を覚える)
  │     (applying・起動中(pending)・すでに deleting なら 409。delete_failed からは、やり直しとして受け付ける)
  ├─ Temporal に DeleteDeployWorkflow の開始を頼む(開始できなければ、status を元に戻す)
  └─ すぐ 200 {}
        ↓ worker が非同期に実行(失敗したらリトライの期限 15 分まで)
  DeleteDeployWorkflow
   ├─ Activity: DeleteDeployment      k8s の Deployment を削除する(Pod も消える。無ければ成功。名前の形が違えば拒否)
   ├─ Activity: DeleteDeployRecord    DB の行を削除する(deleting のものだけ。これで GET が 404 になる)
   └─ 期限までに終わらなければ Activity: MarkDeployDeleteFailed  delete_failed にして、Workflow は失敗で終わる
```

## 5.4 k8s の実際の状態の流れ: watcher

ここまでの流れ(作成、反映、スケール、削除)は、k8s に「頼む」側。k8s の実際の状態を DB に「写す」のは、watcher だけ(詳しくは [watcher.md](watcher.md))。

```
k8s の Pod / Deployment の変化
  → watcher(リーダーの1つだけ。advisory lock) の informer が受け取る
  → 作業キュー → DB に反映
       deploy_observations   Deployment のレプリカ数など(上書き)
       deploy_pods           Pod の現在の状態
       deploy_pod_events     Pod の状態変化のログ(created / ready / restarted / waiting ...)
```

古い Pod のイベントは、watcher が、30 日と 1deploy あたり 1000 件の上限で削除する([watcher.md](watcher.md))。

**API は、deploy の詳細の `pods` を、`deploy_pods` から返す**(k8s には問い合わせない)。
```
GET /deploy/:deployId
  → DeployController.Get
       ├─ DeployService.Get        deploy を読む(他人のものは 404)
       └─ DeployService.ListPods   持ち主を確かめてから、DeployObservationRepository.ListPodsByDeploy で Pod を読む
  → pods: [{podId: "pod-" + k8s の Pod の UID, status: pending / running / terminating}]  作られた順
```
watcher が止まっている間は、古い値(または空)になる。

**ワーカーの起動待ちも、DB の観測値を読む**(`WaitDeploymentReady`。作成、反映、スケールで共通)。
```
ApplyDeployment / ApplyPendingConfig / ApplyScale   Deployment を作る・更新する。更新後の generation を返す
  → WaitDeploymentReady(DeployID, MinGeneration)
       ├─ 心拍が新しい(30 秒未満): DB の deploy_observations で判断(世代が MinGeneration 以上で、レプリカ数が揃う)
       └─ 心拍が古い・無い:       k8s に直接問い合わせる(watcher の障害で、正常な deploy を failed にしない)
```

## 5.5 クラッシュし続ける deploy を止める

```
watcher(リーダーだけ)
  ├─ Pod の保存のたびに、unhealthy_since(安定して動いていない状態が始まった時刻)を更新
  └─ crashloop.Monitor(1分ごと)  止めるべき deploy を探す(安定した Pod が無く、10分以上不健全)
       → Temporal に StopCrashLoopWorkflow の開始を頼む
  ↓ worker が実行
  StopCrashLoopWorkflow
   ├─ Activity: BeginCrashLoopStop   active なら applying にして、crash_loop_limit のイベントを記録(active でなければ終わり)
   ├─ Activity: ScaleToZero           k8s のレプリカを 0 にする(DB の replicas は変えない)
   └─ Activity: FinishCrashLoopStop   failed にする
```
止まったあとは、同じ数のスケール(5.2)で、やり直せる。詳しくは [watcher.md](watcher.md)。

## 5.6 service(ポートの公開): `POST /deploy/:deployId/service`

`POST` と `DELETE` は、deploy の作成・削除と同じ形(受け付けて、ワーカーが非同期に実行)。`GET` は DB を読むだけ。

```
POST /deploy/:deployId/service
  → ServiceController.Create(入力の検査: 名前・番号・protocol・重複)
  → ServiceService.Create
      ├─ ServiceRepository.Create   deploy の行をロックして、持ち主・状態(削除の途中は 409)・すでに service があるか(409)を確かめ、pending で保存
      ├─ Temporal に CreateServiceWorkflow の開始を頼む(開始できなければ、保存を取り消す)
      └─ 200 {serviceId, status: pending, dnsName: null, clusterIP: null}
        ↓ worker が非同期に実行
  CreateServiceWorkflow
   ├─ Activity: EnsureService      DB の service から、k8s の Service(種類は SERVICE_TYPE、selector は deploy の ID)を作る。
   │                                払い出された ClusterIP と、DNS 名(<serviceId>.<projectId>.svc.<CLUSTER_DOMAIN>)を返す
   └─ Activity: MarkServiceResult  成功なら active にして DNS 名と ClusterIP を記録、失敗なら failed

DELETE /deploy/:deployId/service
  → ServiceService.Delete → ServiceRepository.MarkDeleting(deleting にする。作成中・削除中は 409)→ DeleteServiceWorkflow
        ├─ Activity: DeleteServiceObject   k8s の Service を削除(無ければ成功)
        ├─ Activity: DeleteServiceRecord   DB の行を削除(deleting のものだけ)
        └─ 期限(15分)までに終わらなければ MarkServiceDeleteFailed(failed。もう一度削除できる)
```


### ポートの更新・適用・破棄(`PATCH`・`POST .../apply`・`POST .../discard`)

`PATCH` と `discard` は、DB の `pending_ports` を書き換えるだけ(行をロックして、deploy が反映中・削除の途中でないこと、service が active であることを確かめる)。**k8s の Service への反映だけが非同期**。

```
POST /deploy/:deployId/service/apply
  → ServiceService.ApplyPorts
      ├─ ServiceRepository.BeginPortsApply   deploy を applying にする(pending_ports が無い・反映中・起動中・削除の途中・service が active でないなら 409)
      ├─ Temporal に ApplyServicePortsWorkflow の開始を頼む(開始できなければ、deploy の状態を元に戻す)
      └─ すぐ 200 {}
        ↓ worker が非同期に実行
  ApplyServicePortsWorkflow
   ├─ Activity: ApplyServicePorts          pending のポートで、k8s の Service を更新する(Pod は再起動しない)
   ├─ (失敗したときだけ) RollbackServicePorts  k8s の Service を、現在のポートに戻す
   └─ Activity: CommitServicePortsResult    成功: ports ← pending、pending を空に、deploy を active(replicas 0 なら scaled_zero)に戻す
                                             失敗: ports と pending は変えず、deploy を failed にする
```

**deploy を削除するとき**(5.3): `DeleteDeployWorkflow` の最初に、`DeleteDeployService` が、その deploy の service の k8s の Service を消す。DB の service の行は、deploy の行を消すときに、CASCADE で一緒に消える。deploy が削除中の間、`GET` の service の status は、`deleting` として返る。

## 5.7 ingress(外向きの入り口): `/projects/:projectId/ingresses`

ingress は、プロジェクトに属し、Traefik の IngressRoute 1つに対応する。状態の規則と遷移は `model.Ingress` が持つ。

```
POST /projects/:projectId/ingresses                      (作成。Workflow は使わない)
 └─ IngressService.Create
     ├─ ingresshost.Generator.Generate     「形容詞-色-動物.ベースドメイン」を払い出す(払い出さない名前は選び直す)
     └─ IngressRepository.Create           プロジェクトをロックして active を確かめ、保存(host が重なったら作り直す)

PUT /projects/:projectId/ingresses/:ingressId            (ルールを pending として保存)
 └─ model.ValidateIngressRules             path の書式・重複・個数・ポート番号の範囲
     └─ IngressRepository.SavePendingRules 流す先(同じプロジェクトの service の tcp のポート)を確かめて保存。現在と同じ内容なら pending は NULL

POST .../apply
 ├─ IngressRepository.BeginRulesApply      流す先を改めて確かめて(不正なら 409)、applying にする
 └─ TemporalIngressWorkflowStarter.StartRulesApply
     └─ ApplyIngressRulesWorkflow          IngressRoute を pending のルールで更新 → 失敗したら現在のルールに戻す → 結果を確定
         (成功: ルールを昇格して active / 失敗: ルールと pending は変えず failed)

DELETE .../:ingressId
 ├─ IngressRepository.MarkDeleting         deleting にする(applying・deleting は 409)
 └─ DeleteIngressWorkflow                  IngressRoute を削除 → レコードを削除(期限までに終えられなければ failed)
```

- IngressRoute は、ルールが1つ以上あるときだけ存在する。ルールを空にして apply すると、IngressRoute が削除される。
- ルールは `Host(...) && PathPrefix(...)` の式になり、Service のポートへ流す。長い path が優先される。
- service を消す・ポートを減らしたとき、ingress のルールは自動では消えない(流す先を失ったルールは、Traefik が 503 を返す)。次の apply で、流す先の検査により 409 になる。
- k8s の操作は `worker/src/cluster/ingress_operator.go`(動的クライアント)。k8s につながない開発用は、ログだけを出す実装。
- プロジェクトのレコードを消すとき、配下の ingress も一緒に消す(`ProjectRepository.DeleteRecord`)。IngressRoute は namespace の削除で消える。

## 5.8 volume(永続ストレージ)とマウント: `/projects/:projectId/volumes`、`/deploy/:deployId/mounts`

volume は、プロジェクトに属し、k8s の PVC 1つに対応する。状態の規則と遷移は `model.Volume` と、`Deploy` のマウントのメソッド(`deploy_mounts.go`)が持つ。

```
POST /projects/:projectId/volumes                       (作成。Workflow は使わない。PVC もまだ作らない)
 └─ VolumeService.Create  →  VolumeRepository.Create    プロジェクトをロックして active を確かめ、保存(名前の重複は 409)

PATCH .../volumes/:volumeId/size  →  apply  →  discard (拡張。deploy や service と同じ pending → apply)
 └─ POST .../apply
     ├─ VolumeRepository.BeginSizeApply   applying にする
     └─ ApplyVolumeSizeWorkflow           PVC を拡張し、容量が増えるのを待つ(5分まで) → 結果を確定
         (成功: sizeMB を昇格して active / 失敗: sizeMB と pending は変えず failed。PVC は縮められないので、巻き戻さない)

DELETE .../volumes/:volumeId
 ├─ VolumeRepository.MarkDeleting         マウント中(適用待ちも含む)なら 409。deleting にする
 └─ DeleteVolumeWorkflow                  PVC を削除 → レコードを削除(期限までに終えられなければ failed)

PUT /deploy/:deployId/mounts  →  POST .../mounts/apply  →  POST .../mounts/discard
 ├─ DeployRepository.SavePendingMounts    volume が同じプロジェクトにあることを確かめて、pending に保存(volume の行をロックする)
 ├─ DeployRepository.BeginMountsApply     volume を改めて確かめ(不正なら 409)、deploy を applying にする
 └─ ApplyDeployMountsWorkflow             namespace と PVC(無ければ作る)を用意 → pending のマウントで Deployment を更新
                                          → 起動を待つ → 失敗したら元のマウントに戻す → 結果を確定
```

- 現在のマウントは、`newDeploymentDefinition` が `deploy.Mounts` から Deployment の内容に入れる。作成・スケーリング・起動設定の反映・crash-loop の停止の、どの経路でも、マウントが落ちない。
- マウントを持つ Deployment は、更新方式を Recreate にする(ReadWriteOnce の PVC を、古い Pod が持ったままにしないため。既定の ReadWriteMany でも、同じ方式にしている)。
- volume の行は、マウントの保存・適用(行をロック)と、削除の受け付け(行をロック)で、同時には進まない。
- プロジェクトのレコードを消すとき、配下の volume の行も消す。PVC は、namespace の削除で消える。

## 6. どのファイルを見ればよいか

| 知りたいこと | 見るファイル |
|---|---|
| バックエンドの組み立て | `backend/src/main.go`、`backend/src/server/server.go`、`backend/src/server/route_registration.go` |
| ワーカーの組み立て | `worker/src/main.go` |
| URL とハンドラーの対応 | `backend/src/server/route_registration.go` |
| 入力の検証、レスポンスの形 | `backend/src/controller/project_controller.go` |
| ビジネスルール(状態遷移、Workflow の開始) | `backend/src/service/`(`project_service.go`、`deploy_service.go`(作成・取得)と、操作ごとの `deploy_config_service.go`、`deploy_scale_service.go`、`deploy_deletion_service.go`、`temporal_*_starter.go`) |
| DB の読み書き(SQL) | `shared/repository/`(`project_repository.go`。deploy は操作ごとに分かれている: `deploy_repository.go`(作成・取得)、`deploy_config_repository.go`、`deploy_scale_repository.go`、`deploy_deletion_repository.go`、`deploy_crash_loop_repository.go`) |
| Workflow・Activity の名前、入出力の型、タイムアウト | `shared/workflow/delete_project.go`、`create_deploy.go`、`apply_deploy_config.go`、`scale_deploy.go`、`delete_deploy.go`、`crash_loop.go`、`service.go`、`ingress.go`、`volume.go`、`apply_deploy_mounts.go` |
| Workflow(順序、リトライ、補償) | `worker/src/workflow/delete_project_workflow.go`、`create_deploy_workflow.go`、`apply_deploy_config_workflow.go`、`scale_deploy_workflow.go`、`delete_deploy_workflow.go`、`stop_crash_loop_workflow.go`、`create_service_workflow.go`、`delete_service_workflow.go`、`apply_service_ports_workflow.go`、`apply_ingress_rules_workflow.go`、`delete_ingress_workflow.go`、`volume_workflows.go`、`apply_deploy_mounts_workflow.go` |
| Activity(実際の処理) | `worker/src/activity/`(`project_deletion_activities.go`、`create_deploy_activities.go`、`apply_deploy_config_activities.go`、`scale_deploy_activities.go`、`delete_deploy_activities.go`、`crash_loop_activities.go`、`service_activities.go`、`ingress_activities.go`、`volume_activities.go`、`apply_deploy_mounts_activities.go`)、`worker/src/cluster/`(k8s を操作する実装) |
| k8s への接続の設定とクライアント、ラベル名 | `shared/k8s/` |
| watcher(観測、リーダー選出) | `watcher/src/`(`observer/`)、`shared/leader/`、`shared/repository/`(`deploy_observation_repository.go`、`deploy_pod_observation_repository.go`、`crash_loop_candidate_repository.go`、`pod_event_retention_repository.go`)、`pod_event_diff.go` |
| 環境変数の読み方 | `shared/config/`、`backend/src/config/`、`worker/src/config/` |

### ファイルの分け方

## 5.9 ログ: `GET /deploy/:deployId/logs`、deploy の詳細の `logPods`

ログの**書き込み**は、logcollector(別コンテナ)だけ。backend は、DB を**読むだけ**(設計は [logs.md](logs.md))。

```
[書き込み] logcollector(リーダー1つ。advisory lock)
  Manager   2 秒ごとに deploy_pods(watcher が書く)と比べて、Pod ごとに Follower を始める・止める
  Follower  pods/log を follow で購読 → 整える(NUL、長い行、速さの上限)→ 500 行または 1 秒ごとに、
            deploy_logs に追記 + deploy_log_streams の読み取り位置を、同じトランザクションで更新
  Pruner    1 分ごとに、7 日より古い行、Pod ごとの 50MiB を超えた古い行、終了して 7 日たったストリームを削除

[読み取り] curl ──▶ backend
  ① 認証ミドルウェア → ユーザー ID
  ② DeployController.GetLogs   podId(`pod-` + UID)と since を検査する(400)
  ③ DeployLogService.GetLogs   deploy が、ユーザーのものか(404)
                               → DeployLogQueryRepository.ReadLogs: Pod が、その deploy のストリームか(404)、id > since の行を最大 1000 行
                               → 本文の合計が 1MiB を超えるところで切る(少なくとも1行)→ nextSince を決める
  ④ レスポンス: 各行を「JST の時刻 + 空白 + 本文 + 改行」にしてつなげる

GET /deploy/:deployId の logPods: DeployController.Get が、DeployLogService.ListLogPods(行が残っているストリームを、新しい Pod の順に最大 50 件)を呼ぶ。
```

大きくなりすぎないよう、deploy に関するファイルは、**層ごとに、操作(役割)ごとのファイル**に分けてある。同じ型のメソッドが、複数のファイルにまたがる(Go では、同じパッケージなら、問題ない)。

| 層 | 作成・取得(核) | 操作ごと |
|---|---|---|
| controller | `deploy_controller.go`、`deploy_response.go`(レスポンスの形) | `deploy_config_controller.go`、`deploy_scale_controller.go`、`deploy_deletion_controller.go` |
| service | `deploy_service.go`(interface、作成・取得・Pod の一覧) | `deploy_config_service.go`、`deploy_scale_service.go`、`deploy_deletion_service.go` |
| repository | `deploy_repository.go`(作成・取得・状態の更新・行のロック) | `deploy_config_repository.go`、`deploy_scale_repository.go`、`deploy_deletion_repository.go`、`deploy_crash_loop_repository.go` |
| activity | `create_deploy_activities.go` | `deployment_ready_activities.go`(起動待ち)、`deployment_operator.go`(k8s を操作する interface)、`logging_deployment_operator.go` |
| watcher | `observer.go`(Run と準備) | `observer_sync.go`(1つずつ DB に反映)、`observer_workers.go`(キューの処理と心拍) |

テストも、同じ単位のファイルに分けてある(`xxx_test.go`)。

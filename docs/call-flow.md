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
  ② 認証ミドルウェア(今は DEV_USER_ID の仮)が、ユーザー ID をコンテキストに入れる
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
   ├─ Activity: EnsureProjectNamespace  プロジェクトの namespace を、無ければ作る
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
   └─ Activity: CommitConfigResult   結果が決まってから、正式な値を pending の内容にして pending を空にし、
                                      成功なら active(replicas 0 なら scaled_zero)、失敗なら failed にする
```

仕様どおり、**実際の反映を先に試み、結果が確定してから** DB の正式な値を更新する。失敗しても、正式な値は「最後に反映を試みた値」になる。

## 6. どのファイルを見ればよいか

| 知りたいこと | 見るファイル |
|---|---|
| バックエンドの組み立て | `backend/src/main.go`、`backend/src/server/server.go`、`backend/src/server/route_registration.go` |
| ワーカーの組み立て | `worker/src/main.go` |
| URL とハンドラーの対応 | `backend/src/server/route_registration.go` |
| 入力の検証、レスポンスの形 | `backend/src/controller/project_controller.go` |
| ビジネスルール(状態遷移、Workflow の開始) | `backend/src/service/`(`project_service.go`、`deploy_service.go`、`temporal_*_starter.go`) |
| DB の読み書き(SQL) | `shared/repository/`(`project_repository.go`、`deploy_repository.go`) |
| Workflow・Activity の名前、入出力の型、タイムアウト | `shared/workflow/delete_project.go`、`create_deploy.go`、`apply_deploy_config.go` |
| Workflow(順序、リトライ、補償) | `worker/src/workflow/delete_project_workflow.go`、`create_deploy_workflow.go`、`apply_deploy_config_workflow.go` |
| Activity(実際の処理) | `worker/src/activity/`(`project_deletion_activities.go`、`create_deploy_activities.go`、`apply_deploy_config_activities.go`)、`worker/src/cluster/`(k8s を操作する実装) |
| k8s への接続の設定とクライアント | `shared/k8s/` |
| 環境変数の読み方 | `shared/config/`、`backend/src/config/`、`worker/src/config/` |

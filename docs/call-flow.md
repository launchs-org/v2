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
     ├─ chooseNamespaceDeleter(設定)                    ← 「どの実装を使うか」を選ぶ(下の 3 を参照)
     ├─ activity.NewProjectDeletionActivities(namespace を消す役, repository, logger)
     └─ RegisterActivity(...) × 3                      Activity を名前で登録 → Run()
```

## 3. 「選ぶ」処理: `chooseNamespaceDeleter`(以前の factory)

namespace を消す役(`activity.NamespaceDeleter`)には、実装が2つある。どちらを使うかは、環境変数 `KUBERNETES_ACCESS` で決まる。

```
chooseNamespaceDeleter(設定)
 └─ k8s.NewClientset(設定)                              shared/k8s(他の部品でも使える共通部品)
     ├─ none の場合 → ErrNotConnected を返す
     │     → LoggingNamespaceDeleter(何も消さず、警告のログだけ。開発用)
     └─ kubeconfig / in-cluster の場合 → k8s のクライアント
           → KubernetesNamespaceDeleter(client-go で本当に消す)
```

「実装を選ぶ関数」は、ここ1か所だけ。k8s のクライアントの作り方そのものは `shared/k8s` にあり、deploy など別の処理が k8s につなぐときも、同じ `k8s.NewClientset` を使う。

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

## 5. どのファイルを見ればよいか

| 知りたいこと | 見るファイル |
|---|---|
| バックエンドの組み立て | `backend/src/main.go`、`backend/src/server/server.go`、`backend/src/server/route_registration.go` |
| ワーカーの組み立て | `worker/src/main.go` |
| URL とハンドラーの対応 | `backend/src/server/route_registration.go` |
| 入力の検証、レスポンスの形 | `backend/src/controller/project_controller.go` |
| ビジネスルール(状態遷移、Workflow の開始) | `backend/src/service/project_service.go`、`temporal_project_deletion_starter.go` |
| DB の読み書き(SQL) | `shared/repository/project_repository.go` |
| Workflow・Activity の名前、入出力の型、タイムアウト | `shared/workflow/delete_project.go` |
| Workflow(順序、リトライ、補償) | `worker/src/workflow/delete_project_workflow.go` |
| Activity(実際の処理) | `worker/src/activity/project_deletion_activities.go`、`worker/src/cluster/namespace_deleter.go` |
| k8s への接続の設定とクライアント | `shared/k8s/` |
| 環境変数の読み方 | `shared/config/`、`backend/src/config/`、`worker/src/config/` |

# プロジェクトのタスク(Workflow の履歴)

## 1. 目的
deploy の作成や ingress の反映などは、Temporal の Workflow で、裏で動く。これまで、画面には `applying` や `failed` としか出ず、**何が動いているか、なぜ失敗したか**が、Temporal の画面か worker のログを見ないと、分からなかった。Workflow を、プロジェクトの**タスク**として記録し、画面の「タスク」で、状態と失敗の理由を見られるようにする。

## 2. 設計
- **記録するのは worker**。worker の **interceptor**(`worker/src/tracking`)が、Workflow の開始と終了のところに割り込み、ローカルアクティビティで、`project_tasks` に書く。各 Workflow のコードには、手を入れない。
- **対象のプロジェクト**は、Workflow の入力の ID(`ProjectID`、`ApplyID`、`DeployID`、`ServiceID`、`IngressID`、`VolumeID`)から、DB で決める(`ProjectTaskRepository.ResolveProject`)。対象のリソースが無い(もう削除されている)ときは、記録しない。
- **種類**: `shared/projecttask` の対応表(18 種類)。ここに無い Workflow は、記録しない。
- **子の Workflow**(テンプレートの適用が呼ぶ、deploy の作成など)も、別の行として記録し、`parent_workflow_id` に親を持つ。一覧は**トップのタスクだけ**を返し、子は、詳細で返す。
- **記録の失敗は、Workflow を失敗させない**(ログに残すだけ。ローカルアクティビティは、最大 3 回やり直す)。
- **再生の互換**: 記録を足す前に始まった Workflow を壊さないよう、`workflow.GetVersion`(`project-task-tracking`)で、古い履歴は、記録せずに、そのまま再生する。
- **失敗の理由**: Temporal のエラーは、何重にも包まれているので、一番内側のアプリケーションのエラーのメッセージを使う。`shared/projecttask.Sanitize` で、**秘密を含みうる部分を伏せ**(URL の `user:pass@`、`password=…`、`token=…`、`Bearer …`)、1 行にして、500 文字までに切る。取り消しと時間切れは、決まった文にする。
- **lost**: 実行中のまま、2 時間を過ぎたタスクは、画面で「不明」(`lost`)として扱う(worker が止まった、Workflow が期限で終わった、などで、終わりの記録が付かなかったもの)。保存はされず、読み出すときに決まる。

### プロジェクト一覧でも見る(ユーザー単位)
- タスクに、**持ち主(`user_id`)と、記録したときのプロジェクトの名前(`project_name`)**を控える(マイグレーション 22)。プロジェクトが削除されても、**削除のタスクと名前が残る**(保存期間までは、プロジェクトが無くなったタスクも消さない)。
- `GET /tasks` が、ユーザーの全てのプロジェクトのタスクを返す。プロジェクト一覧の画面の下に、「タスク」の表を出す。
- **プロジェクトの作成**は、Workflow ではない(DB に書くだけ)ので、作成のときに、backend が、**成功として 1 回で記録**する(`project_create`。`RecordFinished`)。

### 進行表示(手順 = Activity)
- 実行中の Workflow が、いま、どの手順(Activity)にいるかを、画面に出す。**Temporal の履歴から読み取る**(`GetWorkflowHistory` と `DescribeWorkflowExecution`)。DB には、手順ごとの記録を持たない(履歴が、重くならないように)。
- Workflow ごとの**手順の定義**(成功したときに順に通る Activity と、画面に出す名前)が、`shared/projecttask/progress.go`。定義した手順に、**終わった・動いている(何回目の試行か)・失敗・待ち**の状態をつけ、終わった数 / 総数を、進行バーにする。**失敗のときだけ呼ばれる後始末**(Rollback など)は、定義に入れず、動いたときだけ、「後始末」として、後ろに足す。定義が無い Workflow(テンプレートの適用など)は、総数が決まらないので、動き続ける帯にする(子の Workflow の一覧で、様子が分かる)。
- 一覧には、**実行中のタスク(最大 10 件)**の、総数・終わった数・いまの手順だけを付ける。詳細には、手順の一覧(失敗の理由、やり直しの回数つき)を付ける。**成功したタスクは、Temporal に問い合わせず**、全て終わったものとして出す。履歴が無い(Temporal の保存期間を過ぎた)ものは、進み具合を付けない。
- Temporal への問い合わせは、同じ Workflow について、2 秒間は控えを使う(画面の自動更新の負担を抑える)。実行中のタスクがあるときの画面の更新は、3 秒ごと。

## 3. データ
`project_tasks`(マイグレーション 21、22): `id`、`project_id`、`user_id`、`project_name`、`kind`、`workflow_type`、`resource_id`、`workflow_id`、`run_id`、`parent_workflow_id`、`status`(running、succeeded、failed、canceled)、`error`、`started_at`、`finished_at`。`(workflow_id, run_id)` が一意で、再試行しても、二重にならない。

**保存期間**: 30 日と、プロジェクトごとに 1000 件の、どちらか早い方。プロジェクトが削除されたあとのタスクは、残す(削除の履歴を見るため)。worker が、1 時間ごとに行う(何度行っても同じ結果なので、リーダーは決めない)。

## 4. API
[docs/endpoints/tasks.md](endpoints/tasks.md)。`GET /projects/:projectId/tasks`(状態と種類で絞り込み、ページング、数)、`GET /projects/:projectId/tasks/:taskId`(子つき)。読むだけ。

## 5. 画面
プロジェクトの「タスク」タブと、プロジェクト一覧の下の「タスク」(`frontend/components/views/tasks-view.tsx`。一覧では、全てのプロジェクトの分を、プロジェクト名つきで出し、削除したものには「(削除済み)」と出す)。新しい順の表(開始、種類、対象、状態、進み具合、時間、理由)。実行中は、**回転マーク**と**進行バー**(終わった手順 / 総数と、いまの手順)を出し、3 秒ごとに更新する。行を開くと、**手順の一覧**(終わった=緑のチェック、動いている=回転マーク、失敗=赤、これから=白抜き)が出る。状態(実行中・失敗・成功)と種類で絞り込み、行を開くと、理由全文、Workflow の ID、子のタスクが見える。対象の deploy、ingress、volume は、ボタンで開ける。タブには、直近 24 時間の失敗の数(橙)か、実行中の数(青)が出る。

## 6. これから
- 再実行や取り消しは、まだ無い(見るだけ)。
- 失敗の理由は、アプリケーションのエラーの文を、そのまま整えたもの。画面向けの説明(イベントの `eventHint` のような)は、まだ付けていない。

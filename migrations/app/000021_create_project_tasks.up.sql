-- プロジェクトのタスク(Temporal の Workflow 1 回につき 1 行。dev-docs/tasks.md)。
-- worker の interceptor が、Workflow の開始と終了のときに書く。画面の「タスク」で、状態と失敗の理由を見るために使う。
CREATE TABLE project_tasks (
    -- "task-" + UUID v4。
    id                 text        PRIMARY KEY,
    project_id         text        NOT NULL,
    -- 種類(deploy_create など。shared/projecttask の Kinds)。
    kind               text        NOT NULL,
    -- Workflow の型の名前(CreateDeployWorkflow など)。
    workflow_type      text        NOT NULL,
    -- 対象のリソースの ID(deploy、service、ingress、volume、テンプレートの適用、プロジェクトの ID)。
    resource_id        text        NOT NULL,
    -- Temporal の Workflow の ID と、実行の ID(Temporal の画面で探すために残す)。
    workflow_id        text        NOT NULL,
    run_id             text        NOT NULL,
    -- 親の Workflow の ID(子の Workflow のとき。トップのタスクは空)。
    parent_workflow_id text        NOT NULL DEFAULT '',
    -- running、succeeded、failed、canceled のどれか。
    status             text        NOT NULL,
    -- 失敗の理由(整えたもの。秘密を含みうる部分は、伏せてある)。成功なら空。
    error              text        NOT NULL DEFAULT '',
    started_at         timestamptz NOT NULL,
    finished_at        timestamptz,
    UNIQUE (workflow_id, run_id)
);
-- プロジェクトごとの一覧(新しい順)に使う。
CREATE INDEX project_tasks_list_idx ON project_tasks (project_id, started_at DESC, id DESC);
-- 子のタスクを、親から引く。
CREATE INDEX project_tasks_parent_idx ON project_tasks (parent_workflow_id) WHERE parent_workflow_id <> '';
-- 保存期間で消す。
CREATE INDEX project_tasks_started_idx ON project_tasks (started_at);

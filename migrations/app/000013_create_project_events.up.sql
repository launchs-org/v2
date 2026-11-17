-- プロジェクトの namespace で起きた、k8s の Event(docs/project-events.md)。書き手は watcher だけ。backend は読むだけ。
-- k8s の Event は、約 1 時間で消えるので、ここに溜めて、過去も見られるようにする。

CREATE TABLE project_events (
    id             bigserial   PRIMARY KEY,
    -- namespace(プロジェクトの ID)。プロジェクトの行は、削除のあとも、イベントを残すため、外部キーにしない
    -- (「なぜ消えたか」を見られる。保存期間と件数の上限で、掃除する)。
    project_id     text        NOT NULL,
    -- k8s の Event の UID。同じ Event は、k8s が、回数(count)を増やして更新するので、これで 1 行にまとめる。
    event_uid      text        NOT NULL UNIQUE,
    -- Normal / Warning
    type           text        NOT NULL,
    -- 理由(Pulling、Failed、BackOff、FailedScheduling など)と、本文。
    reason         text        NOT NULL DEFAULT '',
    message        text        NOT NULL DEFAULT '',
    -- 対象(Pod、Deployment、ReplicaSet、PersistentVolumeClaim など)の種類と名前。
    object_kind    text        NOT NULL DEFAULT '',
    object_name    text        NOT NULL DEFAULT '',
    -- 対象が、deploy に属する(launchs-org/deploy-id のラベルが付く)ときの、deploy の ID。属さなければ空。
    deploy_id      text        NOT NULL DEFAULT '',
    -- 同じ Event が、何回起きたか。
    count          integer     NOT NULL DEFAULT 1,
    first_seen_at  timestamptz NOT NULL,
    last_seen_at   timestamptz NOT NULL
);
-- プロジェクトごとの一覧(新しい順。同じ時刻は、id で順序を決める)。
CREATE INDEX project_events_project_idx ON project_events (project_id, last_seen_at DESC, id DESC);
-- deploy での絞り込み。
CREATE INDEX project_events_deploy_idx ON project_events (project_id, deploy_id, last_seen_at DESC) WHERE deploy_id <> '';
-- 保存期間の削除に使う。
CREATE INDEX project_events_last_seen_idx ON project_events (last_seen_at);

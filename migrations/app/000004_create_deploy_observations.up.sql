-- watcher が書く、k8s の実際の状態(観測値)のテーブル。書き手は watcher だけ。
-- どれも deploy の行を消すと、一緒に消える(ON DELETE CASCADE)。

-- Deployment の状態。deploy 1つに1行。
CREATE TABLE deploy_observations (
    deploy_id            text        PRIMARY KEY REFERENCES deploys (id) ON DELETE CASCADE,
    -- Deployment の最新の設定の世代(metadata.generation)と、k8s が処理済みの世代(status.observedGeneration)。
    generation           bigint      NOT NULL,
    observed_generation  bigint      NOT NULL,
    -- status の各レプリカ数(Pod の総数、最新の設定の Pod の数、準備完了の数、使える数)。
    replicas             integer     NOT NULL,
    updated_replicas     integer     NOT NULL,
    ready_replicas       integer     NOT NULL,
    available_replicas   integer     NOT NULL,
    -- watcher が、この値を観測した時刻。
    observed_at          timestamptz NOT NULL
);

-- Pod の現在の状態。Pod 1つに1行(Pod が消えると、行も消える)。
CREATE TABLE deploy_pods (
    -- k8s の Pod の UID。Pod が作り直されると、別の UID になる。
    pod_uid                 text        PRIMARY KEY,
    deploy_id               text        NOT NULL REFERENCES deploys (id) ON DELETE CASCADE,
    -- Pod の namespace(プロジェクトの ID)と、Pod の名前。
    project_id              text        NOT NULL,
    pod_name                text        NOT NULL,
    -- pending / running / terminating。deploy の詳細の pods に出す値。
    status                  text        NOT NULL CHECK (status IN ('pending', 'running', 'terminating')),
    -- k8s の Pod の phase(Pending、Running、Succeeded、Failed、Unknown)。
    phase                   text        NOT NULL,
    -- Ready の条件が True か。
    ready                   boolean     NOT NULL,
    -- コンテナの再起動の回数の合計。
    restart_count           integer     NOT NULL,
    -- いま待っている理由(CrashLoopBackOff、ImagePullBackOff、ContainerCreating など)。待っていなければ空。
    waiting_reason          text        NOT NULL DEFAULT '',
    -- 最後に終了したときの理由(OOMKilled、Error など)と、終了コード。無ければ空と NULL。
    last_termination_reason text        NOT NULL DEFAULT '',
    last_exit_code          integer,
    node_name               text        NOT NULL DEFAULT '',
    -- k8s が Pod を作った時刻と、コンテナが起動した時刻(まだなら NULL)。
    pod_created_at          timestamptz NOT NULL,
    started_at              timestamptz,
    observed_at             timestamptz NOT NULL
);
CREATE INDEX deploy_pods_deploy_id_idx ON deploy_pods (deploy_id);
CREATE UNIQUE INDEX deploy_pods_project_id_pod_name_idx ON deploy_pods (project_id, pod_name);

-- Pod の状態変化のログ(作成、準備完了、クラッシュ、再起動、終了など)。追記だけ。
CREATE TABLE deploy_pod_events (
    id           bigserial   PRIMARY KEY,
    deploy_id    text        NOT NULL REFERENCES deploys (id) ON DELETE CASCADE,
    pod_uid      text        NOT NULL,
    pod_name     text        NOT NULL,
    -- created / ready / not_ready / restarted / waiting / terminating / deleted
    type         text        NOT NULL,
    -- 理由(OOMKilled、CrashLoopBackOff など)と、補足(終了コードなど)。無ければ空。
    reason       text        NOT NULL DEFAULT '',
    message      text        NOT NULL DEFAULT '',
    occurred_at  timestamptz NOT NULL
);
CREATE INDEX deploy_pod_events_deploy_id_id_idx ON deploy_pod_events (deploy_id, id DESC);

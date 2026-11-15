-- deploy のコンテナの出力ログ(docs/logs.md)。書き手は logcollector だけ。backend は読むだけ。
-- どちらも deploy の行を消すと、一緒に消える(ON DELETE CASCADE)。

-- Pod 1つの、収集の状態。Pod が消えても残る(保持期間の間)。podId の持ち主の確認にも使う。
CREATE TABLE deploy_log_streams (
    -- k8s の Pod の UID(API の podId は、"pod-" + この値)。
    pod_uid        text        PRIMARY KEY,
    deploy_id      text        NOT NULL REFERENCES deploys (id) ON DELETE CASCADE,
    -- Pod の namespace(プロジェクトの ID)と、Pod の名前。
    project_id     text        NOT NULL,
    pod_name       text        NOT NULL,
    -- Pod の作成時刻(deploy_pods.pod_created_at の写し)。
    started_at     timestamptz NOT NULL,
    -- 収集を終えた時刻(Pod が消えた、またはコンテナが終了した)。収集中は NULL。
    finished_at    timestamptz,
    -- 読み取り位置: 最後に保存した行の時刻(unix ナノ秒)と、その時刻の行を、何行まで保存したか。
    resume_ns      bigint      NOT NULL DEFAULT 0,
    resume_count   integer     NOT NULL DEFAULT 0,
    -- 最後に処理した、コンテナの再起動の回数(deploy_pods.restart_count)。増えたら、前のコンテナのログを読む。
    restart_count  integer     NOT NULL DEFAULT 0,
    -- 保存済みの行の長さの合計(バイト)。Pod ごとの上限(50MB)の判定に使う。
    bytes          bigint      NOT NULL DEFAULT 0
);
-- deploy ごとの一覧(logPods)と、終了したストリームの削除に使う。
CREATE INDEX deploy_log_streams_deploy_idx ON deploy_log_streams (deploy_id, started_at DESC);
CREATE INDEX deploy_log_streams_finished_idx ON deploy_log_streams (finished_at) WHERE finished_at IS NOT NULL;

-- ログの1行。id は、全体で増える番号(API の since / nextSince になる。間が抜けてよい)。
CREATE TABLE deploy_logs (
    id         bigserial PRIMARY KEY,
    pod_uid    text      NOT NULL REFERENCES deploy_log_streams (pod_uid) ON DELETE CASCADE,
    -- 行の時刻(k8s のログの時刻。unix ナノ秒)。timestamptz はマイクロ秒までなので、使わない。
    logged_ns  bigint    NOT NULL,
    line       text      NOT NULL
);
CREATE INDEX deploy_logs_pod_idx ON deploy_logs (pod_uid, id);
-- 保存期間の削除に使う。
CREATE INDEX deploy_logs_logged_idx ON deploy_logs (logged_ns);

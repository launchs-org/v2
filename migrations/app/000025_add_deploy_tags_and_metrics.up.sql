-- deploy のタグ(文字列の JSON の配列。小文字の英数字と . _ : -)。タグ単位の一斉削除と、メトリクスの集計に使う。
-- テンプレートから作った deploy には、「template:<テンプレートの id>」と「apply:<適用の識別>」が自動で付く。
ALTER TABLE deploys ADD COLUMN tags jsonb NOT NULL DEFAULT '[]'::jsonb;
-- タグで絞る(tags @> '["x"]')ための索引。
CREATE INDEX deploys_tags_idx ON deploys USING gin (tags);

-- deploy ごとの、CPU・メモリなどの観測値(watcher が、一定の間隔で追記する。古い行は watcher が消す)。
CREATE TABLE deploy_metric_samples (
    -- 観測した deploy。deploy が消えたら、行も消える。
    deploy_id       text        NOT NULL REFERENCES deploys (id) ON DELETE CASCADE,
    -- deploy の属するプロジェクト(集計の絞り込み用)。
    project_id      text        NOT NULL,
    -- 観測した時刻(同じ回の観測は、全ての deploy で同じ時刻)。
    sampled_at      timestamptz NOT NULL,
    -- 全ての Pod の CPU の使用量の合計(ミリコア)。metrics-server が無いときは NULL。
    cpu_millicores  bigint,
    -- 全ての Pod のメモリの使用量の合計(バイト)。metrics-server が無いときは NULL。
    memory_bytes    bigint,
    -- Pod の数と、Ready の Pod の数。
    pods            integer     NOT NULL DEFAULT 0,
    ready_pods      integer     NOT NULL DEFAULT 0,
    -- コンテナの再起動の回数の合計(いま存在する Pod の分)。
    restarts        integer     NOT NULL DEFAULT 0,
    PRIMARY KEY (deploy_id, sampled_at)
);
-- プロジェクトごとの、時間の範囲での集計に使う。
CREATE INDEX deploy_metric_samples_project_idx ON deploy_metric_samples (project_id, sampled_at);

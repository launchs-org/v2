-- ingress のアクセスの集計(Traefik のプラグイン launchs-stats が 1 分ごとに出し、accessstats が保存する。docs/ingress-plugins.md)。
-- minutes は 1 分ごと(Traefik の Pod ごと)の行。hours は、minutes を 1 時間ごとにまとめた行(replica は空文字)。
-- ingress の削除では消さず、保存期間で消える(集計の行は、ingress の外に出さない)。
CREATE TABLE ingress_access_minutes (
    ingress_id    text             NOT NULL,
    -- 出力した Traefik の Pod の名前。
    replica       text             NOT NULL,
    -- この集計の始まりの時刻。
    bucket_start  timestamptz      NOT NULL,
    interval_sec  integer          NOT NULL,
    -- リクエストの数。
    count         bigint           NOT NULL,
    -- {"1xx":n,"2xx":n,...}。
    status        jsonb            NOT NULL,
    -- {"GET":n,"POST":n,...}。
    methods       jsonb            NOT NULL,
    -- 応答の大きさの合計(バイト)。
    bytes         bigint           NOT NULL,
    -- 応答時間の度数(区切りは shared/accessstats の LatencyBoundsMs。最後は、それ以上)。
    latency       jsonb            NOT NULL,
    -- 応答時間の合計(ミリ秒)。
    sum_ms        double precision NOT NULL,
    -- [{"key":"/a/:id","count":n},...](上位)。
    paths         jsonb            NOT NULL,
    -- [{"key":"203.0.113.9","count":n},...](上位。保存しない設定では空)。
    clients       jsonb            NOT NULL,
    unique_clients integer         NOT NULL,
    PRIMARY KEY (ingress_id, replica, bucket_start)
);
CREATE INDEX ingress_access_minutes_start_idx ON ingress_access_minutes (bucket_start);

CREATE TABLE ingress_access_hours (
    ingress_id    text             NOT NULL,
    replica       text             NOT NULL DEFAULT '',
    bucket_start  timestamptz      NOT NULL,
    interval_sec  integer          NOT NULL,
    count         bigint           NOT NULL,
    status        jsonb            NOT NULL,
    methods       jsonb            NOT NULL,
    bytes         bigint           NOT NULL,
    latency       jsonb            NOT NULL,
    sum_ms        double precision NOT NULL,
    paths         jsonb            NOT NULL,
    clients       jsonb            NOT NULL,
    unique_clients integer         NOT NULL,
    PRIMARY KEY (ingress_id, replica, bucket_start)
);
CREATE INDEX ingress_access_hours_start_idx ON ingress_access_hours (bucket_start);

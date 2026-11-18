-- ingress のアクセスログ(1 リクエストにつき 1 行。Traefik のプラグイン launchs-stats が出し、accessstats が保存する。docs/ingress-plugins.md)。
-- 保存は、保存期間と、ingress ごとの行数の上限の、どちらか早い方で消える。URL は、クエリも含めて、そのまま保存する。
CREATE TABLE ingress_access_logs (
    id           bigserial        PRIMARY KEY,
    ingress_id   text             NOT NULL,
    -- 出力した Traefik の Pod の名前。
    replica      text             NOT NULL,
    -- プラグインの 1 回の出力の始まりの時刻と、その中での番号(同じ行を二重に保存しないための印)。
    batch_start  timestamptz      NOT NULL,
    seq          integer          NOT NULL,
    -- リクエストを受けた時刻(ミリ秒の精度)。
    requested_at timestamptz      NOT NULL,
    method       text             NOT NULL,
    -- パスとクエリ。
    url          text             NOT NULL,
    -- ステータス(WebSocket など、記録できないものは 0)。
    status       integer          NOT NULL,
    duration_ms  double precision NOT NULL,
    -- クライアントの IP(保存しない設定では空)。
    client_ip    text             NOT NULL,
    bytes        bigint           NOT NULL,
    UNIQUE (ingress_id, replica, batch_start, seq)
);
CREATE INDEX ingress_access_logs_list_idx ON ingress_access_logs (ingress_id, requested_at DESC, id DESC);
CREATE INDEX ingress_access_logs_time_idx ON ingress_access_logs (requested_at);

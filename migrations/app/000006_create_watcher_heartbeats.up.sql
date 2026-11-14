-- watcher が生きていることを示す、心拍の記録。1行だけ(id は常に 1)。
-- ワーカーは、この記録が新しいときだけ、DB の観測値を信用する。古いときは、k8s に直接問い合わせる。
-- 時刻は、サーバーごとの時計のずれを避けるため、データベースの時計(now())で書き、データベースの時計で比べる。
CREATE TABLE watcher_heartbeats (
    id       smallint    PRIMARY KEY CHECK (id = 1),
    beat_at  timestamptz NOT NULL
);

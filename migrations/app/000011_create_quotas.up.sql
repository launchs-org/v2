-- アカウントごとの上限(クォータ)。値はデータなので、更新すれば変わる(docs/auth.md §8)。
-- 超えたら、作成・拡張が 409 になる。既存のデータが、あとから下げた上限を超えていても、消さない(新しく作れないだけ)。

-- 全ユーザーの既定。1行だけ(id は常に true)。
CREATE TABLE quota_defaults (
    id                   boolean PRIMARY KEY DEFAULT true CHECK (id),
    -- プロジェクトの数(deleting 以外)。
    max_projects         integer NOT NULL CHECK (max_projects >= 0),
    -- deploy の数(deleting 以外)。service は deploy に1つなので、数えない。
    max_deploys          integer NOT NULL CHECK (max_deploys >= 0),
    -- 全 deploy の「replicas × スペックの CPU」の合計(ミリコア)。
    max_cpu_millicores   integer NOT NULL CHECK (max_cpu_millicores >= 0),
    -- 全 deploy の「replicas × スペックのメモリ」の合計(MiB)。
    max_memory_mib       integer NOT NULL CHECK (max_memory_mib >= 0),
    -- volume の数。
    max_volumes          integer NOT NULL CHECK (max_volumes >= 0),
    -- volume の大きさの合計(MB)。拡張の pending も、予約として数える。
    max_volume_total_mb  integer NOT NULL CHECK (max_volume_total_mb >= 0)
);

INSERT INTO quota_defaults (id, max_projects, max_deploys, max_cpu_millicores, max_memory_mib, max_volumes, max_volume_total_mb)
VALUES (true, 10, 20, 4000, 8192, 10, 20480);

-- ユーザーごとの上書き。NULL の列は、既定を使う。行は、最初の作成のときに(ロックのために)作られる。
CREATE TABLE account_quotas (
    user_id              text        PRIMARY KEY,
    max_projects         integer     CHECK (max_projects >= 0),
    max_deploys          integer     CHECK (max_deploys >= 0),
    max_cpu_millicores   integer     CHECK (max_cpu_millicores >= 0),
    max_memory_mib       integer     CHECK (max_memory_mib >= 0),
    max_volumes          integer     CHECK (max_volumes >= 0),
    max_volume_total_mb  integer     CHECK (max_volume_total_mb >= 0),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

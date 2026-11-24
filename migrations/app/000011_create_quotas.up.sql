-- アカウントごとの上限(クォータ)。値はデータなので、更新すれば変わる(dev-docs/auth.md §8)。
-- 超えたら、作成・拡張が 409 になる。既存のデータが、あとから下げた上限を超えていても、消さない(新しく作れないだけ)。

-- スペックごとの、1アカウントが作れる deploy の数の既定(「large は 5 個まで」)。スペックを足すときに、上限も決める。
-- replicas(スケール)は、数えない。スケールの上限は、アプリ側の定数(1つの deploy で最大 5)。
ALTER TABLE deploy_specs ADD COLUMN default_max_deploys integer NOT NULL DEFAULT 0 CHECK (default_max_deploys >= 0);
UPDATE deploy_specs SET default_max_deploys = 20 WHERE name = 'small';
UPDATE deploy_specs SET default_max_deploys = 10 WHERE name = 'medium';
UPDATE deploy_specs SET default_max_deploys = 5 WHERE name = 'large';

-- 全ユーザーの既定。1行だけ(id は常に true)。
CREATE TABLE quota_defaults (
    id                   boolean PRIMARY KEY DEFAULT true CHECK (id),
    -- プロジェクトの数(deleting 以外)。
    max_projects         integer NOT NULL CHECK (max_projects >= 0),
    -- deploy の合計の数(deleting 以外。スペックごとの数とは別)。service は deploy に1つなので、数えない。
    max_deploys          integer NOT NULL CHECK (max_deploys >= 0),
    -- volume の数。
    max_volumes          integer NOT NULL CHECK (max_volumes >= 0),
    -- volume の大きさの合計(MB)。拡張の pending も、予約として数える。
    max_volume_total_mb  integer NOT NULL CHECK (max_volume_total_mb >= 0)
);

INSERT INTO quota_defaults (id, max_projects, max_deploys, max_volumes, max_volume_total_mb)
VALUES (true, 10, 20, 10, 20480);

-- ユーザーごとの上書き。NULL の列は、既定を使う。行は、最初の作成のときに(ロックのために)作られる。
CREATE TABLE account_quotas (
    user_id              text        PRIMARY KEY,
    max_projects         integer     CHECK (max_projects >= 0),
    max_deploys          integer     CHECK (max_deploys >= 0),
    max_volumes          integer     CHECK (max_volumes >= 0),
    max_volume_total_mb  integer     CHECK (max_volume_total_mb >= 0),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- スペックごとの、ユーザーごとの上書き。行が無いスペックは、deploy_specs.default_max_deploys を使う。
CREATE TABLE account_spec_quotas (
    user_id              text        NOT NULL,
    spec                 text        NOT NULL,
    max_deploys          integer     NOT NULL CHECK (max_deploys >= 0),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, spec)
);

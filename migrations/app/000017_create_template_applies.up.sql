-- テンプレートから部品を作る、1 回の実行(docs/templates.md 段階 C)。計画(展開済みの部品の一覧)と、進み具合を持つ。
CREATE TABLE template_applies (
    -- "apply-" + UUID v4。アプリ側で作る。
    id          text        PRIMARY KEY,
    -- 属するプロジェクトと、持ち主のユーザー ID、使ったテンプレートの id。
    project_id  text        NOT NULL,
    user_id     text        NOT NULL,
    template_id text        NOT NULL,
    -- running: 作成中 / succeeded: 全て完了 / failed: どれかが失敗(作れたものは、残る)。
    status      text        NOT NULL DEFAULT 'running'
                CHECK (status IN ('running', 'succeeded', 'failed')),
    -- 全ての変数の値(自動生成を含む。パスワードも、平文で入る。env と同じ扱い)。
    values      jsonb       NOT NULL,
    -- 計画: 展開済みの、作る部品の一覧(順番と、部品の ID を含む)。shared/templateplan の Plan。
    plan        jsonb       NOT NULL,
    -- ステップごとの進み具合(計画と同じ順)。shared/templateplan の StepState の配列。
    steps       jsonb       NOT NULL,
    -- 失敗の理由(failed のときだけ)。
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- プロジェクトごとの一覧(新しい順)に使う。
CREATE INDEX template_applies_project_created_idx ON template_applies (project_id, created_at DESC, id DESC);

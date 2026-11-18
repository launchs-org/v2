-- プロジェクトで共有する ConfigMap(設定ファイルの組。docs/configmap.md)。
-- 編集のたびに、新しい版(configmap_revisions の行)が増える。k8s の ConfigMap は、deploy が適用するときに、版ごとに作る(段階 2)。
CREATE TABLE configmaps (
    -- "configmap-" + UUID v4。アプリ側で作る。
    id               text        PRIMARY KEY,
    -- 属するプロジェクトと、持ち主のユーザー ID。
    project_id       text        NOT NULL,
    user_id          text        NOT NULL,
    -- プロジェクトの中で一意の名前(英数字とハイフン、64文字まで)。
    name             text        NOT NULL,
    -- 最新の版の番号(1 から。編集のたびに +1)。
    current_revision integer     NOT NULL DEFAULT 1 CHECK (current_revision >= 1),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);
-- プロジェクトごとの一覧(作成日時の降順)に使う。
CREATE INDEX configmaps_project_created_idx ON configmaps (project_id, created_at DESC, id DESC);

-- ConfigMap の版。行は追記だけで、更新しない。
CREATE TABLE configmap_revisions (
    configmap_id text        NOT NULL REFERENCES configmaps (id) ON DELETE CASCADE,
    -- 版の番号(1 から)。
    revision     integer     NOT NULL CHECK (revision >= 1),
    -- ファイルの配列。各要素は {"key", "encoding"("text" か "base64"), "content"}。key の昇順で保存する。
    files        jsonb       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (configmap_id, revision)
);

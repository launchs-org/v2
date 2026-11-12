-- プロジェクトのテーブル。build / deploy などは、全ていずれかのプロジェクトに属する。
CREATE TABLE projects (
    -- "project-" + UUID v4。アプリ側で作る。
    id         text        PRIMARY KEY,
    -- 持ち主のユーザー ID。認証は別のデータベースなので、外部キーは張らない。
    user_id    text        NOT NULL,
    -- 英数字とハイフンのみ、1〜64文字(最小長は仮)。
    name       text        NOT NULL CHECK (name ~ '^[A-Za-z0-9-]{1,64}$'),
    -- active: 通常 / deleting: 削除を受け付けて、実体を削除している最中。
    status     text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleting')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- 同じユーザーの中で、名前は重複できない(409)。
    CONSTRAINT projects_user_id_name_key UNIQUE (user_id, name)
);

-- 一覧(作成日時の降順 + カーソルによるページング)のための索引。
CREATE INDEX projects_user_id_created_at_idx ON projects (user_id, created_at DESC, id DESC);

-- deploy のスペック(small / medium / large)のマスターデータ。CPU とメモリの大きさを持つ。
-- 値はデータなので、後から変えられる。deploy 作成時に、名前で参照する。
CREATE TABLE deploy_specs (
    name            text    PRIMARY KEY CHECK (name ~ '^[a-z0-9-]{1,32}$'),
    -- CPU の大きさ(ミリコア。250 = 0.25 CPU)。
    cpu_millicores  integer NOT NULL CHECK (cpu_millicores > 0),
    -- メモリの大きさ(MiB)。
    memory_mib      integer NOT NULL CHECK (memory_mib > 0)
);

INSERT INTO deploy_specs (name, cpu_millicores, memory_mib) VALUES
    ('small', 250, 256),
    ('medium', 500, 512),
    ('large', 1000, 1024);

-- deploy(イメージから起動するコンテナ)のテーブル。k8s の Deployment 1つに対応する。
CREATE TABLE deploys (
    -- "deploy-" + UUID v4。アプリ側で作る。k8s の Deployment の名前にも使う。
    id              text        PRIMARY KEY,
    -- 属するプロジェクト。外部キーは張らず、アプリ側で守る。
    project_id      text        NOT NULL,
    -- 持ち主のユーザー ID(プロジェクトの持ち主と同じ。所有の確認を簡単にするために持つ)。
    user_id         text        NOT NULL,
    -- 起動するイメージ。
    image_ref       text        NOT NULL CHECK (image_ref <> ''),
    -- スペックの名前(deploy_specs.name)。作成後は変えられない。
    spec            text        NOT NULL,
    -- 希望するレプリカ数。0 にスケールすることで「停止」を表す。
    replicas        integer     NOT NULL DEFAULT 1 CHECK (replicas >= 0),
    -- pending: 作成を受け付けて、まだ起動が確認できていない / active: 稼働中 / applying: 設定の反映中 /
    -- failed: 起動または反映に失敗 / scaled_zero: replicas が 0 / deleting: 削除中 / delete_failed: 削除に失敗して止まっている
    status          text        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'active', 'applying', 'failed', 'scaled_zero', 'deleting', 'delete_failed')),
    -- 起動設定。現在コンテナに反映されている値。
    env             jsonb       NOT NULL DEFAULT '{}'::jsonb,
    command         text,
    args            jsonb,
    -- 起動設定の、まだ適用していない変更。3つは常にセットで、変更が無ければ全て NULL。
    pending_env     jsonb,
    pending_command text,
    pending_args    jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- プロジェクトの deploy 履歴(作成日時の降順 + カーソルによるページング)のための索引。
CREATE INDEX deploys_project_id_created_at_idx ON deploys (project_id, created_at DESC, id DESC);

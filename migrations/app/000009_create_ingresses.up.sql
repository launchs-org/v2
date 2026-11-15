-- プロジェクトの外向きの入り口(Traefik の IngressRoute 1つに対応する)。ホスト名(host)は、ランダムな単語で自動で払い出す。
-- プロジェクトの行を消すときは、アプリ側で、配下のイングレスも一緒に消す(deploys と同じ)。
CREATE TABLE ingresses (
    -- "ingress-" + UUID v4。アプリ側で作る。IngressRoute の名前にも使う。
    id            text        PRIMARY KEY,
    -- 属するプロジェクト(k8s の namespace の名前でもある)と、持ち主のユーザー ID。
    project_id    text        NOT NULL,
    user_id       text        NOT NULL,
    -- 払い出したホスト名(例: brave-blue-fox.launchs.org)。全イングレスで一意。
    host          text        NOT NULL UNIQUE,
    -- 現在反映されているルールの一覧(JSON の配列。各要素は path、serviceId、port)。
    rules         jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- まだ適用していないルールの一覧。変更が無ければ NULL(空の配列は「ルールを全て消す」変更)。
    pending_rules jsonb,
    -- active: 反映済み / applying: 反映中 / failed: 反映または削除に失敗 / deleting: 削除中。
    status        text        NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active', 'applying', 'failed', 'deleting')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- プロジェクトごとの一覧(作成日時の降順)に使う。
CREATE INDEX ingresses_project_created_idx ON ingresses (project_id, created_at DESC, id DESC);

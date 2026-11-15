-- プロジェクトの永続ストレージ(k8s の PVC 1つに対応する)。deploy を消してもデータは残り、別の deploy に付け替えられる。
-- PVC は、最初にマウントを適用するとき(namespace がある状態)に作る。プロジェクトの行を消すときは、アプリ側で、配下のボリュームも消す。
CREATE TABLE volumes (
    -- "volume-" + UUID v4。アプリ側で作る。PVC の名前にも使う。
    id              text        PRIMARY KEY,
    -- 属するプロジェクト(k8s の namespace の名前でもある)と、持ち主のユーザー ID。
    project_id      text        NOT NULL,
    user_id         text        NOT NULL,
    -- プロジェクトの中で一意の名前(英数字とハイフン、64文字まで)。
    name            text        NOT NULL,
    -- 現在有効な大きさ(MB)と、まだ適用していない大きさ。変更が無ければ pending は NULL。
    size_mb         integer     NOT NULL CHECK (size_mb > 0),
    pending_size_mb integer     CHECK (pending_size_mb > 0),
    -- active: 使える / applying: 拡張を反映中 / failed: 拡張または削除に失敗 / deleting: 削除中。
    status          text        NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'applying', 'failed', 'deleting')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);
-- プロジェクトごとの一覧(作成日時の降順)に使う。
CREATE INDEX volumes_project_created_idx ON volumes (project_id, created_at DESC, id DESC);

-- deploy がマウントするボリューム。JSON の配列(各要素は volumeId、mountPath)。NULL は、マウント無し。
-- pending_mounts は、まだ適用していない構成。変更が無ければ NULL(空の配列は「全て外す」変更)。
ALTER TABLE deploys ADD COLUMN mounts jsonb;
ALTER TABLE deploys ADD COLUMN pending_mounts jsonb;

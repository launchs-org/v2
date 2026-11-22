-- deploy のイメージの更新履歴。イメージを入れ替える反映(config/apply の流れ)が終わるたびに、1 行足す。
-- 成功と失敗の両方を残す(失敗のときは、試したイメージが分かる)。
CREATE TABLE deploy_image_history (
    -- 行の連番(新しい順に並べるのに使う)。
    id                 bigserial   PRIMARY KEY,
    -- 対象の deploy の ID。deploy を消したら、履歴も消す。
    deploy_id          text        NOT NULL REFERENCES deploys (id) ON DELETE CASCADE,
    -- 入れ替える前のイメージ。
    previous_image_ref text        NOT NULL,
    -- 入れ替えようとしたイメージ。
    image_ref          text        NOT NULL,
    -- 反映の結果(applied か failed)。
    result             text        NOT NULL,
    -- 結果が確定した日時。
    created_at         timestamptz NOT NULL DEFAULT now()
);
-- deploy ごとの、新しい順の一覧に使う。
CREATE INDEX deploy_image_history_deploy_idx ON deploy_image_history (deploy_id, id DESC);

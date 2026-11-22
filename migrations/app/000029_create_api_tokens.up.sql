-- API トークン(機械が、利用者として API を呼ぶための JWT)の、有効・無効の管理。
-- 許す操作や対象は、トークンの claim にあるので、ここでは持たない(一覧の表示用に、写しだけ持つ)。
-- トークンの本体は保存しない。
CREATE TABLE api_tokens (
    -- トークンの jti("apitoken-" + UUID)。失効と、認証のときの有効確認に使う。
    id           text        PRIMARY KEY,
    -- 持ち主のユーザー ID。トークンを使った操作は、この利用者の操作として扱う。
    user_id      text        NOT NULL,
    -- 利用者が付けた名前(用途のメモ)。
    name         text        NOT NULL,
    -- 許す操作の写し(一覧の表示用)。
    scope        jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- 対象の deploy とプロジェクトの写し(一覧の表示用)。
    deploys      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    projects     jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- 期限。NULL なら無期限。
    expires_at   timestamptz,
    -- 失効した日時。NULL なら有効。
    revoked_at   timestamptz,
    -- 最後に使った日時(1 分より短い間隔では更新しない)。
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
-- 利用者ごとの一覧に使う。
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id, created_at DESC);

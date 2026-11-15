-- 認証コンテナのデータ(auth データベース)。アプリのデータベースとは、別のデータベース。
-- 仕様は docs/endpoints/auth.md、設計は docs/auth.md。

-- ユーザー。
CREATE TABLE users (
    -- "userid-" + UUID v4。アプリ側で作る。トークンの sub になる。
    id               text        PRIMARY KEY,
    -- 利用者が指定した名前(表示用)と、重複の判定に使う小文字の名前(大文字小文字を区別しない)。
    user_name        text        NOT NULL,
    user_name_lower  text        NOT NULL UNIQUE,
    -- argon2id のハッシュ(パラメータごと、文字列に入れる)。
    password_hash    text        NOT NULL,
    -- active: 使える / banned: 停止中(ログインできず、refresh token も無効)。
    status           text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'banned')),
    -- user: 一般 / admin: 管理者(access token の role になる)。
    role             text        NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- refresh token の有効・無効の管理(jti ごと)。ログアウトで revoked_at を入れる。BAN のときは、そのユーザーの全てに入れる。
CREATE TABLE refresh_tokens (
    jti         text        PRIMARY KEY,
    user_id     text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    issued_at   timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz
);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_expires_idx ON refresh_tokens (expires_at);

-- ユーザー名ごとのログイン失敗の回数とロック。存在しないユーザー名にも、同じように記録する(存在を推測されないため)。
CREATE TABLE login_failures (
    user_name_lower text        PRIMARY KEY,
    failed_count    integer     NOT NULL DEFAULT 0,
    locked_until    timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- IP ごとのレート制限(スライディングウィンドウ)。古い行は、定期的に消す。
CREATE TABLE rate_limit_events (
    id           bigserial   PRIMARY KEY,
    kind         text        NOT NULL CHECK (kind IN ('login', 'signup')),
    ip           text        NOT NULL,
    occurred_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX rate_limit_events_lookup_idx ON rate_limit_events (kind, ip, occurred_at);

-- 招待制のときの招待。コードは、ハッシュ(SHA-256 の16進数)で保存する。
CREATE TABLE invitations (
    code_hash   text        PRIMARY KEY,
    created_by  text,
    expires_at  timestamptz NOT NULL,
    used_by     text,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- ログイン履歴。サインアップとログインの、成功と失敗を、1回ごとに残す(利用者が自分の履歴を見られる。管理者が不正の調査に使う)。
-- レート制限に当たった試行は、残さない(攻撃で、履歴が膨らまないように)。古い行は、定期的に消す。
CREATE TABLE login_events (
    id               bigserial   PRIMARY KEY,
    -- 持ち主のユーザー。存在しないユーザー名への試行は、NULL(user_name_lower だけが入る)。
    user_id          text        REFERENCES users (id) ON DELETE CASCADE,
    -- 試行で指定された名前(小文字。64文字以内のものだけ)。
    user_name_lower  text        NOT NULL,
    kind             text        NOT NULL CHECK (kind IN ('signup', 'login')),
    result           text        NOT NULL CHECK (result IN ('success', 'failure')),
    -- 失敗の理由: unknown_user / wrong_password / locked / banned / invalid_input(unknown_user と invalid_input は user_id が NULL で、誰の履歴にも出ない)。成功のときは NULL。
    reason           text,
    ip               text        NOT NULL,
    user_agent       text        NOT NULL DEFAULT '',
    occurred_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_events_user_idx ON login_events (user_id, occurred_at DESC, id DESC);
CREATE INDEX login_events_occurred_idx ON login_events (occurred_at);

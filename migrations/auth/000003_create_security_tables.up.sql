-- セキュリティ: IP のブロックと、管理操作の監査ログ(docs/security.md)。

-- ブロックする IP(1 つの IP か、範囲)。ログインとサインアップを、拒否する。期限が無ければ、解除するまで続く。
CREATE TABLE blocked_ips (
    -- "block-" + UUID v4。
    id              text        PRIMARY KEY,
    -- ブロックする範囲(1 つの IP は、/32 か /128)。
    network         cidr        NOT NULL,
    -- 理由(管理者が書く)。
    reason          text        NOT NULL DEFAULT '',
    -- ブロックした管理者。
    created_by      text        NOT NULL,
    created_by_name text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- 期限。NULL なら、解除するまで。
    expires_at      timestamptz
);

-- 管理操作の監査ログ(誰が、いつ、何を、何に対して)。追記だけで、更新も削除もしない。
CREATE TABLE admin_audit_logs (
    id          bigserial   PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    -- 操作した管理者の ID と名前(コマンドからの操作は、ID が "command")。
    actor_id    text        NOT NULL,
    actor_name  text        NOT NULL DEFAULT '',
    -- 操作の名前(user_ban、invitation_create、ip_block など)。
    action      text        NOT NULL,
    -- 操作の対象(ユーザー ID、招待 ID、IP など)。
    target      text        NOT NULL DEFAULT '',
    -- 補足(変更の内容など。秘密は入れない)。
    detail      text        NOT NULL DEFAULT '',
    -- 操作した接続元の IP。
    ip          text        NOT NULL DEFAULT ''
);
CREATE INDEX admin_audit_logs_occurred_idx ON admin_audit_logs (occurred_at DESC, id DESC);

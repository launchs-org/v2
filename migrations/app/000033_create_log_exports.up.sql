-- アクセスログの、外への書き出し(webhook と Google Drive)。設計は dev-docs/log-export.md。

-- アカウントの Google Drive の接続(1 アカウントに 1 つ)。合鍵(リフレッシュトークン)は、暗号化して持つ。
CREATE TABLE google_drive_connections (
    -- 持ち主のユーザー ID。認証は別のデータベースなので、外部キーは張らない。
    user_id        text        PRIMARY KEY,
    -- つないだ Google アカウントのメールアドレス(表示用)。
    account_email  text        NOT NULL DEFAULT '',
    -- 暗号化した合鍵(shared/credential)。
    credential     bytea       NOT NULL,
    -- launchs-access-logs フォルダの ID(Drive の中の、書き出し先の大元)。
    root_folder_id text        NOT NULL DEFAULT '',
    -- active: 使える / reauth_required: 合鍵が効かない(利用者が、再接続する)。
    status         text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reauth_required')),
    -- 最後のエラー(利用者に見せる短い理由)。
    last_error     text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- 転送の設定 1 つ(ある ingress のアクセスログを、ある送り先に送る)。
CREATE TABLE export_destinations (
    -- "export-" + UUID v4。
    id              text        PRIMARY KEY,
    -- 持ち主のユーザー ID(アカウントごとに 10 個までの数え方に使う)。
    user_id         text        NOT NULL,
    project_id      text        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    -- 対象の ingress。ingress を消したら、転送の設定も消す。
    ingress_id      text        NOT NULL REFERENCES ingresses (id) ON DELETE CASCADE,
    -- webhook / google_drive。
    kind            text        NOT NULL CHECK (kind IN ('webhook', 'google_drive')),
    enabled         boolean     NOT NULL DEFAULT true,
    -- 種類ごとの設定。webhook は url と intervalSeconds。
    config          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- webhook の署名の秘密(暗号化)。Google Drive は、アカウントの接続を使うので、空。
    credential      bytea,
    -- active: 動作中 / failing: 失敗が続いている(再試行する) / reauth_required: Google の再接続が必要。
    status          text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'failing', 'reauth_required')),
    -- 送り済みの、ingress_access_logs.id の最後。作ったときの最大の id から始める。
    last_cursor     bigint      NOT NULL DEFAULT 0,
    last_success_at timestamptz,
    -- 最後のエラー(利用者に見せる短い理由)。
    last_error      text        NOT NULL DEFAULT '',
    failure_count   integer     NOT NULL DEFAULT 0,
    -- 次に動く時刻(exporter が、これを過ぎたものを動かす)。
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX export_destinations_user_idx ON export_destinations (user_id);
CREATE INDEX export_destinations_ingress_idx ON export_destinations (ingress_id, created_at);
CREATE INDEX export_destinations_due_idx ON export_destinations (next_attempt_at) WHERE enabled;
-- 同じ ingress に、Google Drive への転送は、1 つまで。
CREATE UNIQUE INDEX export_destinations_one_drive_idx ON export_destinations (ingress_id) WHERE kind = 'google_drive';

-- Drive の、プロジェクトごとのフォルダ(アカウントの接続の下)。
CREATE TABLE export_drive_folders (
    user_id    text NOT NULL,
    project_id text NOT NULL,
    folder_id  text NOT NULL,
    PRIMARY KEY (user_id, project_id)
);

-- Drive に書いた、1 日分のファイル(続きを足すときに、どのファイルかを知るため)。
CREATE TABLE export_drive_files (
    destination_id text    NOT NULL REFERENCES export_destinations (id) ON DELETE CASCADE,
    -- 日本時間の日付。
    day            date    NOT NULL,
    -- 大きくなりすぎたときの分割の番号(1 から)。
    part           integer NOT NULL DEFAULT 1,
    file_id        text    NOT NULL,
    bytes          bigint  NOT NULL DEFAULT 0,
    log_rows       bigint  NOT NULL DEFAULT 0,
    -- このファイルに書いた、ログの id の最後(書き込みをやり直したときに、同じ行を二重に書かないため)。
    last_log_id    bigint  NOT NULL DEFAULT 0,
    PRIMARY KEY (destination_id, day, part)
);

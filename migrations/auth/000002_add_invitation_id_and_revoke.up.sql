-- 招待に、管理 API で扱うための ID と、メモ、取り消しの日時を足す(docs/auth.md §6)。
ALTER TABLE invitations ADD COLUMN id text;
-- 既にある招待(あれば)にも ID を付ける。
UPDATE invitations SET id = 'invitation-' || gen_random_uuid()::text WHERE id IS NULL;
ALTER TABLE invitations ALTER COLUMN id SET NOT NULL;
ALTER TABLE invitations ADD CONSTRAINT invitations_id_key UNIQUE (id);
-- 管理者が付けるメモ(誰に渡すか、など)。
ALTER TABLE invitations ADD COLUMN note text NOT NULL DEFAULT '';
-- 取り消した日時。取り消した招待は、使えない。
ALTER TABLE invitations ADD COLUMN revoked_at timestamptz;
-- 一覧(新しい順)に使う。
CREATE INDEX invitations_created_idx ON invitations (created_at DESC, id DESC);

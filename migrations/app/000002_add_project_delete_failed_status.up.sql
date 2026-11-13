-- プロジェクトの status に、delete_failed(削除に失敗して止まっている状態)を加える。
-- 削除の Workflow が、リトライの期限までに削除を終えられなかったときに、この状態にする。
-- この状態からは、もう一度削除を呼べる(再実行)。
ALTER TABLE projects DROP CONSTRAINT projects_status_check;
ALTER TABLE projects ADD CONSTRAINT projects_status_check
    CHECK (status IN ('active', 'deleting', 'delete_failed'));

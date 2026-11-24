-- タスクに、持ち主と、プロジェクトの名前を、控える(dev-docs/tasks.md)。
-- プロジェクト一覧の画面で、全てのプロジェクトのタスクを見るため(user_id)と、プロジェクトが削除されたあとも、削除のタスクに名前を出すため(project_name)。
ALTER TABLE project_tasks ADD COLUMN user_id text NOT NULL DEFAULT '';
ALTER TABLE project_tasks ADD COLUMN project_name text NOT NULL DEFAULT '';
-- すでにある行は、プロジェクトから、埋める(プロジェクトが無くなっているものは、空のまま)。
UPDATE project_tasks SET user_id = projects.user_id, project_name = projects.name FROM projects WHERE projects.id = project_tasks.project_id;
-- ユーザーごとの一覧(新しい順)に使う。
CREATE INDEX project_tasks_user_idx ON project_tasks (user_id, started_at DESC, id DESC);

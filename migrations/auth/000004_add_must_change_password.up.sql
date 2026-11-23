-- 管理者が一時パスワードを発行したユーザーは、最初に、自分のパスワードに変える(画面が、変更の画面を出す)。
ALTER TABLE users ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;

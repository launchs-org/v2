-- 開発環境の PostgreSQL に、各コンテナが使うデータベースを作る。
-- このファイルは、データのボリュームを初めて作るときだけ実行される。
-- やり直したいときは、ボリュームごと消す(task reset)。

-- API とワーカーが使うデータベース。
CREATE DATABASE app;

-- 認証コンテナが使うデータベース。
CREATE DATABASE auth;

-- Temporal が使うデータベース(スキーマは dev/temporal/setup-schema.sh が作る)。
CREATE DATABASE temporal;
CREATE DATABASE temporal_visibility;

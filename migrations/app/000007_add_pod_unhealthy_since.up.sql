-- Pod が「安定して動いている」状態でなくなった時刻。クラッシュし続ける Pod を見つけるために、watcher が書く。
-- 安定して動いている(Ready のまま、コンテナが5分以上動き続けている)ときは NULL。
ALTER TABLE deploy_pods ADD COLUMN unhealthy_since timestamptz;

-- deploy がマウントする、共有の ConfigMap のファイル(dev-docs/configmap.md)。JSON の配列。
-- 各要素は {"configMapId", "revision", "key", "path", "mode"}。NULL は、マウント無し。
-- pending_configmap_mounts は、まだ適用していない構成。変更が無ければ NULL(空の配列は「全て外す」変更)。
ALTER TABLE deploys ADD COLUMN configmap_mounts jsonb;
ALTER TABLE deploys ADD COLUMN pending_configmap_mounts jsonb;

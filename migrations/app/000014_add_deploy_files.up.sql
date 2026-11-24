-- deploy の「ファイル」(dev-docs/templates.md の段階 A)。設定ファイルなどを、k8s の ConfigMap として、コンテナにマウントする。
-- files は、現在コンテナに反映されている内容([{"path": "/etc/app.conf", "content": "..."}])。NULL は、ファイル無し。
-- pending_files は、まだ適用していない内容。変更が無ければ NULL(空の配列は「全て外す」変更)。env、mounts と同じ。
ALTER TABLE deploys ADD COLUMN files jsonb;
ALTER TABLE deploys ADD COLUMN pending_files jsonb;

-- ingress ごとの、アクセスの集計(Traefik のプラグイン launchs-stats。docs/ingress-plugins.md)を、有効にするか。
-- 既定は有効。既存の ingress も true になるが、IngressRoute に反映されるのは、次に適用(または切り替え)したとき。
ALTER TABLE ingresses ADD COLUMN access_stats boolean NOT NULL DEFAULT true;

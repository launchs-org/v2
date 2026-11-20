-- volume、service、ingress にも、タグ(文字列の JSON の配列)を付ける。deploy と同じ形(000025)。
-- テンプレートから作ったものには、「template:<テンプレートの id>」と「apply:<適用の識別>」が自動で付く。
ALTER TABLE volumes   ADD COLUMN tags jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE services  ADD COLUMN tags jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE ingresses ADD COLUMN tags jsonb NOT NULL DEFAULT '[]'::jsonb;
-- タグで絞る(tags @> '["x"]')ための索引。
CREATE INDEX volumes_tags_idx   ON volumes   USING gin (tags);
CREATE INDEX services_tags_idx  ON services  USING gin (tags);
CREATE INDEX ingresses_tags_idx ON ingresses USING gin (tags);

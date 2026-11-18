-- 保存期間を過ぎた Pod のイベントを、古い順にまとめて削除するための索引。
CREATE INDEX deploy_pod_events_occurred_at_idx ON deploy_pod_events (occurred_at);

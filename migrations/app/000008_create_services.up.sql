-- deploy のポートを、クラスター内へ公開する service(k8s の Service 1つに対応する)。1 deploy につき 1つだけ。
-- deploy の行を消すと、一緒に消える(ON DELETE CASCADE)。
CREATE TABLE services (
    -- "service-" + UUID v4。アプリ側で作る。k8s の Service の名前にも使う。
    id            text        PRIMARY KEY,
    -- 属する deploy(1 deploy = 1 service なので、UNIQUE)。
    deploy_id     text        NOT NULL UNIQUE REFERENCES deploys (id) ON DELETE CASCADE,
    -- 属するプロジェクト(k8s の namespace の名前でもある)と、持ち主のユーザー ID。所有の確認を簡単にするために持つ。
    project_id    text        NOT NULL,
    user_id       text        NOT NULL,
    -- 現在反映されているポートの一覧(JSON の配列。各要素は name、port、protocol)。
    ports         jsonb       NOT NULL,
    -- まだ適用していないポートの一覧。変更が無ければ NULL。
    pending_ports jsonb,
    -- pending: 作成を受け付けて、まだ k8s に作れていない / active: 作成済み / failed: 作成または削除に失敗 / deleting: 削除中。
    status        text        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'active', 'failed', 'deleting')),
    -- k8s が払い出した DNS 名と ClusterIP。active になるまで NULL。
    dns_name      text,
    cluster_ip    text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

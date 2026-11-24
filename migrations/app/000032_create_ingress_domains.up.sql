-- ingress に付ける、利用者のドメイン(カスタムドメイン)。
-- DNS で、ドメインの持ち主であることを確かめる(verified_at が入る)までは、ルーティングに使わない。
CREATE TABLE ingress_domains (
    -- ドメインの ID("domain-" + UUID v4)。
    id          text        PRIMARY KEY,
    -- 付け先の ingress の ID。ingress を消したら、ドメインも消す。
    ingress_id  text        NOT NULL REFERENCES ingresses (id) ON DELETE CASCADE,
    -- ドメイン名(小文字、末尾のドットなし)。
    hostname    text        NOT NULL,
    -- 所有を確かめるための、この申請だけの値(DNS の TXT に置いてもらう)。
    token       text        NOT NULL,
    -- 所有を確かめた日時。NULL は、確認待ち。
    verified_at timestamptz,
    -- 申請した日時。
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- 同じ ingress に、同じドメインを 2 つは付けない。
CREATE UNIQUE INDEX ingress_domains_ingress_hostname_idx ON ingress_domains (ingress_id, hostname);
-- 確認済みのドメインは、全体で 1 つの ingress だけ(他人のドメインを先に押さえられないよう、確認待ちは重なってよい)。
CREATE UNIQUE INDEX ingress_domains_verified_hostname_idx ON ingress_domains (hostname) WHERE verified_at IS NOT NULL;

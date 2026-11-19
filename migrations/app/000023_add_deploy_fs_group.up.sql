-- deploy の Pod の、ボリュームの所有グループ(fsGroup)。テンプレートが、非 root のイメージ(Grafana、n8n など)に、ボリュームへ書かせるために指定する。
-- NULL なら、指定しない(今までどおり)。利用者が API で変えることはできない(テンプレートが、作るときに決める)。
ALTER TABLE deploys ADD COLUMN fs_group integer;

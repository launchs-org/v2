#!/bin/sh
# Temporal の2つのデータベース(temporal と temporal_visibility)に、スキーマを作る。
# docker-compose.yaml の temporal-schema サービスが、Temporal を起動する前に1回だけ実行する。
# 何度実行しても壊れない。スキーマが最新なら、何も変えずに終わる。

set -eu

host=postgres
port=5432
user=postgres
password=postgres
schema_root=/etc/temporal/schema/postgresql/v12

# 引数: データベース名、スキーマのフォルダ名(temporal か visibility)
setup_schema() {
  database_name="$1"
  schema_folder="$2"

  echo "スキーマを作る: ${database_name}"
  temporal-sql-tool --plugin postgres12 --endpoint "${host}" --port "${port}" \
    --user "${user}" --password "${password}" --database "${database_name}" \
    setup-schema --version 0.0 || echo "(スキーマの管理テーブルは作成済み。続ける)"
  temporal-sql-tool --plugin postgres12 --endpoint "${host}" --port "${port}" \
    --user "${user}" --password "${password}" --database "${database_name}" \
    update-schema --schema-dir "${schema_root}/${schema_folder}/versioned"
}

setup_schema temporal temporal
setup_schema temporal_visibility visibility

echo "Temporal のスキーマの準備ができた"

#!/bin/sh
# 開発用の自己署名の証明書(と秘密鍵)を作る。
# docker-compose.yaml の nginx-certificate サービスが、nginx を起動する前に1回だけ実行する。
# 証明書は名前付きボリュームに置き、リポジトリには入れない(秘密鍵をコミットしないため)。
#
# すでに証明書があり、30日以上先まで有効なときは、何もしない。
# 期限が近いとき(または無いとき)は、作り直す。

set -eu

certificate_directory=/certs
certificate_path="${certificate_directory}/dev.crt"
key_path="${certificate_directory}/dev.key"

# 30日(秒)。これより先まで有効なら、作り直さない。
renew_margin_seconds=2592000

if [ -s "${certificate_path}" ] && [ -s "${key_path}" ] \
  && openssl x509 -checkend "${renew_margin_seconds}" -noout -in "${certificate_path}" >/dev/null; then
  echo "証明書は作成済みで、期限にも余裕がある: ${certificate_path}"
  exit 0
fi

echo "自己署名の証明書を作る: ${certificate_path}"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
  -keyout "${key_path}" -out "${certificate_path}" -days 365 \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"

chmod 600 "${key_path}"
chmod 644 "${certificate_path}"
echo "証明書を作った(localhost と 127.0.0.1 で有効。有効期間は365日)"

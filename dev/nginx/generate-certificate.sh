#!/bin/sh
# 開発用の自己署名の証明書(と秘密鍵)を作る。
# docker-compose.yaml の nginx-certificate サービスが、nginx を起動する前に1回だけ実行する。
# 証明書は名前付きボリュームに置き、リポジトリには入れない(秘密鍵をコミットしないため)。
#
# すでに証明書があり、30日以上先まで有効で、公開するアドレス(CERTIFICATE_EXTRA_IP)でも有効なときは、何もしない。
# 期限が近いとき、公開するアドレスが変わったとき(または証明書が無いとき)は、作り直す。

set -eu

certificate_directory=/certs
certificate_path="${certificate_directory}/dev.crt"
key_path="${certificate_directory}/dev.key"

# 30日(秒)。これより先まで有効なら、作り直さない。
renew_margin_seconds=2592000

# 公開するアドレス(IP)。127.0.0.1 以外のときだけ、証明書の IP の一覧に足す。
extra_ip="${CERTIFICATE_EXTRA_IP:-127.0.0.1}"
subject_alt_names="DNS:localhost,IP:127.0.0.1"
if [ "${extra_ip}" != "127.0.0.1" ]; then
  subject_alt_names="${subject_alt_names},IP:${extra_ip}"
fi

if [ -s "${certificate_path}" ] && [ -s "${key_path}" ] \
  && openssl x509 -checkend "${renew_margin_seconds}" -noout -in "${certificate_path}" >/dev/null \
  && openssl x509 -noout -ext subjectAltName -in "${certificate_path}" | grep -q "IP Address:${extra_ip}\b"; then
  echo "証明書は作成済みで、期限にも余裕がある: ${certificate_path}"
  exit 0
fi

echo "自己署名の証明書を作る: ${certificate_path}"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
  -keyout "${key_path}" -out "${certificate_path}" -days 365 \
  -subj "/CN=localhost" \
  -addext "subjectAltName=${subject_alt_names}"

chmod 600 "${key_path}"
chmod 644 "${certificate_path}"
echo "証明書を作った(${subject_alt_names} で有効。有効期間は365日)"

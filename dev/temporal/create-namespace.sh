#!/bin/sh
# Temporal に default の namespace を作る(新しいサーバーは自動では作らないため)。
# docker-compose.yaml の temporal-namespace サービスが、Temporal を起動したあとに1回だけ実行する。
# 何度実行しても壊れない。すでにあれば、何もせずに終わる。

set -eu

address=temporal:7233
namespace=default

echo "Temporal が使えるようになるのを待つ"
until temporal operator cluster health --address "${address}" 2>/dev/null | grep -q SERVING; do
  sleep 2
done

if temporal operator namespace describe --address "${address}" --namespace "${namespace}" >/dev/null 2>&1; then
  echo "namespace は作成済み: ${namespace}"
else
  echo "namespace を作る: ${namespace}"
  temporal operator namespace create --address "${address}" --namespace "${namespace}"
fi

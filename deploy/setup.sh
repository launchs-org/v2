#!/usr/bin/env bash
# launchs を、クラスターに、最初から全部入れる(何度実行しても、同じ結果になる)。
#
# やること(順番):
#   1. namespace と、秘密(settings/secret.yaml。無ければ、乱数で作る)と、設定(ConfigMap)を適用する
#   2. cloudflared のトークンの Secret を作る(環境変数 TUNNEL_TOKEN があるとき)
#   3. Argo CD の Application を作り、順に同期する(traefik → launchs-cluster → launchs-prod)
#
# 使い方:
#   ./setup.sh                          # いまの kubectl の接続先に入れる(確認がある)
#   TUNNEL_TOKEN=xxxx ./setup.sh -y     # cloudflared のトークンを渡し、確認を省く
#   ./setup.sh --secrets-only           # settings/secret.yaml を作るだけ(クラスターには、触らない)
#   ./setup.sh --no-sync                # Application を作るところまで(同期は、Argo CD の画面で行う)
#
# 前提: kubectl、openssl、Argo CD(namespace argocd)。詳しくは deploy.md。

set -euo pipefail

# このスクリプトのあるフォルダ(deploy/)。どこから実行しても、同じ場所を見る。
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# 同期を待つ最長の時間(秒)。
SYNC_TIMEOUT=900

# 引数を読む。
ASSUME_YES=false
SECRETS_ONLY=false
NO_SYNC=false
for argument in "$@"; do
  case "$argument" in
    -y|--yes) ASSUME_YES=true ;;
    --secrets-only) SECRETS_ONLY=true ;;
    --no-sync) NO_SYNC=true ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "不明な引数: $argument(-h で使い方)" >&2; exit 2 ;;
  esac
done

# 進み具合を、1 行で出す。
step() { printf '\n==> %s\n' "$1"; }

# 必要なコマンドが無ければ、止まる。
require() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 が見つかりません。入れてから、もう一度実行してください。" >&2; exit 1; }
}

# settings/secret.yaml を、雛形と乱数から作る(既にあれば、何もしない。秘密は、画面に出さない)。
make_secret_file() {
  local target="$SCRIPT_DIR/settings/secret.yaml"
  local example="$SCRIPT_DIR/settings/secret.example.yaml"
  # 既にあるものは、上書きしない(パスワードや鍵が、変わってしまうため)。
  if [ -f "$target" ]; then
    echo "settings/secret.yaml は、既にあります(そのまま使います)。"
    return
  fi
  # 乱数を作る(DB のパスワードは、英数字だけ)。
  local postgres_password api_token_secret admin_password private_key
  postgres_password="$(openssl rand -hex 24)"
  api_token_secret="$(openssl rand -hex 64)"
  admin_password="$(openssl rand -hex 16)"
  private_key="$(openssl genpkey -algorithm ed25519)"
  # 雛形の placeholder を、値に置き換える(署名鍵は、PEM の 3 行を、まとめて置き換える)。
  awk -v key="$private_key" '
    /-----BEGIN PRIVATE KEY-----/ { skipping = 1; n = split(key, lines, "\n"); for (i = 1; i <= n; i++) print "    " lines[i]; next }
    skipping && /-----END PRIVATE KEY-----/ { skipping = 0; next }
    skipping { next }
    { print }
  ' "$example" \
    | sed -e "s|CHANGE_ME_hex_64bytes_or_more|$api_token_secret|" \
          -e "s|CHANGE_ME_long_random_password|$admin_password|" \
          -e "s|CHANGE_ME_hex\"|$postgres_password\"|" \
    > "$target"
  chmod 600 "$target"
  echo "settings/secret.yaml を作りました(乱数。Git には入りません)。"
  echo "最初の管理者のパスワードは、そのファイルの AUTH_BOOTSTRAP_ADMIN_PASSWORD に入っています。"
}

# Argo CD の Application を、同期して、終わるまで待つ。
sync_application() {
  local name="$1"
  step "同期: $name"
  # Git の最新を取り込ませてから、同期を始める。
  kubectl -n argocd annotate application "$name" argocd.argoproj.io/refresh=hard --overwrite >/dev/null
  sleep 5
  kubectl -n argocd patch application "$name" --type merge -p '{"operation":{"sync":{}}}' >/dev/null
  # 同期の処理が、終わる(Running でなくなる)まで待つ。
  local waited=0 phase=""
  while [ "$waited" -lt "$SYNC_TIMEOUT" ]; do
    phase="$(kubectl -n argocd get application "$name" -o jsonpath='{.status.operationState.phase}' 2>/dev/null || true)"
    case "$phase" in
      Succeeded) echo "$name: 同期できました。"; return ;;
      Failed|Error) echo "$name: 同期に失敗しました。" >&2
        kubectl -n argocd get application "$name" -o jsonpath='{.status.operationState.message}{"\n"}' >&2; exit 1 ;;
    esac
    sleep 5; waited=$((waited + 5))
  done
  echo "$name: $SYNC_TIMEOUT 秒待っても、終わりません(Argo CD の画面で確認してください)。" >&2
  exit 1
}

# ---- 秘密だけ作るモード(クラスターには、触らない) ----
if [ "$SECRETS_ONLY" = true ]; then
  require openssl
  make_secret_file
  exit 0
fi

require kubectl
require openssl

# ---- 接続先の確認(間違ったクラスターに入れないように) ----
step "接続先の確認"
CONTEXT="$(kubectl config current-context)"
echo "接続先: $CONTEXT"
if [ "$ASSUME_YES" != true ]; then
  read -r -p "このクラスターに、launchs を入れますか?(yes と入力): " answer
  [ "$answer" = "yes" ] || { echo "中止しました。"; exit 1; }
fi
# Argo CD が無ければ、止まる(Application を作れない)。
kubectl get crd applications.argoproj.io >/dev/null 2>&1 || { echo "Argo CD が入っていません(CRD applications.argoproj.io が無い)。先に Argo CD を入れてください。" >&2; exit 1; }

# ---- 1. namespace、秘密、設定 ----
step "namespace と設定"
kubectl apply -f "$SCRIPT_DIR/namespace.yaml"
make_secret_file
kubectl apply -k "$SCRIPT_DIR/settings/"

# ---- 2. cloudflared のトークン ----
step "cloudflared のトークン"
kubectl create namespace cloudflared --dry-run=client -o yaml | kubectl apply -f - >/dev/null
if [ -n "${TUNNEL_TOKEN:-}" ]; then
  # トークンは、コマンドの引数に出さず、標準入力から渡す(プロセス一覧に残らない)。
  kubectl -n cloudflared create secret generic tunnel-token --from-file=token=<(printf '%s' "$TUNNEL_TOKEN") --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "Secret tunnel-token を作りました。"
elif kubectl -n cloudflared get secret tunnel-token >/dev/null 2>&1; then
  echo "Secret tunnel-token は、既にあります。"
else
  echo "注意: Secret tunnel-token が無いので、cloudflared は起動しません。TUNNEL_TOKEN=... を付けて、もう一度実行してください。" >&2
fi

# ---- 3. Argo CD の Application ----
step "Application を作る"
kubectl apply -f "$SCRIPT_DIR/argocd/applications.yaml"
if [ "$NO_SYNC" = true ]; then
  echo "同期は、Argo CD の画面で行ってください(順序: traefik → launchs-cluster → launchs-prod)。"
  exit 0
fi
sync_application traefik
sync_application launchs-cluster
sync_application launchs-prod

# ---- 終わり ----
step "起動の確認"
kubectl -n launchs rollout status deploy/backend --timeout=600s
echo
echo "完了しました。https://<ホスト名>/healthz が 200 なら、動いています。"
echo "Cloudflare の設定(トンネルの公開ホスト名など)は、deploy.md を見てください。"

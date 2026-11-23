#!/usr/bin/env python3
"""Traefik の Helm の values に足す、launchs のプラグイン(inlinePlugin)の設定を、JSON(YAML として読める)で出力する。

使い方: python3 traefik-plugins/render_helm_values.py > plugins-values.json
        helm upgrade traefik traefik/traefik -n traefik --version <今の版> -f <今の values> -f plugins-values.json
ソースは、このフォルダの launchs-edge と launchs-stats のファイル(テストを除く)。ソースを変えたら、作り直して、helm upgrade する。
"""
import json
import pathlib

# このスクリプトのあるフォルダ。
ROOT = pathlib.Path(__file__).resolve().parent
# プラグイン名と、Go のモジュール名。
PLUGINS = {"launchs-edge": "github.com/launchs-org/launchs-edge", "launchs-stats": "github.com/launchs-org/launchs-stats"}


def source_of(name: str) -> dict:
    """プラグインのソース(Go のファイル、go.mod、.traefik.yml。テストを除く)を、ファイル名 → 内容の表にする。"""
    files = {}
    for path in sorted((ROOT / name).iterdir()):
        if path.is_file() and (path.suffix == ".go" and not path.name.endswith("_test.go") or path.name in ("go.mod", ".traefik.yml")):
            files[path.name] = path.read_text()
    return files


values = {
    "experimental": {"localPlugins": {
        name: {"type": "inlinePlugin", "moduleName": module, "mountPath": f"/plugins-local/src/{module}", "source": source_of(name)}
        for name, module in PLUGINS.items()
    }},
    # launchs-edge は、全てのリクエストに掛ける(entryPoint の middleware。<namespace>-<名前>@kubernetescrd)。
    "ports": {"web": {"http": {"middlewares": ["traefik-launchs-edge@kubernetescrd"]}}},
}
# Traefik の基本の設定(クラスターで動いているものと同じ。Argo CD が、チャートと一緒に、これを同期する)。
BASE = {
    # Datadog の admission webhook が、Pod に hostPath を差し込んで、PodSecurity に拒否されるのを避ける。
    "deployment": {"podLabels": {"admission.datadoghq.com/enabled": "false"}},
    # 入口は cloudflared(Cloudflare Tunnel)。web(HTTP)だけを、NodePort で出し、websecure は出さない。
    "ports": {"web": {"nodePort": 30080}, "websecure": {"expose": {"default": False}}},
    "service": {"spec": {"type": "NodePort"}},
    # launchs は IngressRoute(CRD)だけを使う。標準の Ingress は、読まない。
    "providers": {"kubernetesIngress": {"enabled": False}},
}


def merge(base: dict, extra: dict) -> dict:
    """辞書を、深く合わせる(同じキーの辞書は、中まで合わせ、それ以外は、extra で上書きする)。"""
    merged = dict(base)
    for key, value in extra.items():
        merged[key] = merge(merged[key], value) if isinstance(value, dict) and isinstance(merged.get(key), dict) else value
    return merged


def render(full: bool) -> str:
    """values を、JSON(YAML として読める)の文字列にする。full なら、基本の設定も含める。"""
    return json.dumps(merge(BASE, values) if full else values, ensure_ascii=False, indent=1) + "\n"


if __name__ == "__main__":
    import sys

    args = sys.argv[1:]
    # --check <ファイル>: ファイルが、いまのソースから作った values(基本の設定込み)と同じか確かめる(CI 用。違えば、1 で終わる)。
    if args[:1] == ["--check"] and len(args) == 2:
        if pathlib.Path(args[1]).read_text() != render(True):
            sys.exit(f"{args[1]} が、プラグインのソースと合っていません。`python3 traefik-plugins/render_helm_values.py --full > {args[1]}` で作り直してください。")
        print("ok")
    else:
        # --full: 基本の設定も含める(Argo CD が使うファイル用)。付けないと、プラグインの設定だけ(helm の既存の values に、重ねる用)。
        sys.stdout.write(render("--full" in args))

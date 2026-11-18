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
print(json.dumps(values, ensure_ascii=False, indent=1))

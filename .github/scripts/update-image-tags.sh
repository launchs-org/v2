#!/usr/bin/env bash
# deploy/kustomization.yaml の、10 個のイメージの newTag を、指定のタグに書き換える。
# 使い方: update-image-tags.sh <タグ(sha-…)> <kustomization.yaml のパス>
# release.yml が、イメージを出したあとに使う。手元でも、同じように試せる。
set -euo pipefail

tag="${1:?使い方: update-image-tags.sh <タグ> <ファイル>}"
file="${2:?使い方: update-image-tags.sh <タグ> <ファイル>}"

# タグは、sha-<16 進の 7〜40 文字> だけ(latest などの、動くタグは、固定にならないので、受け付けない)。
if [[ ! "$tag" =~ ^sha-[0-9a-f]{7,40}$ ]]; then
  echo "タグの形が正しくありません(sha-<16進の7〜40文字>): $tag" >&2
  exit 1
fi

# 書き換える行(イメージ 1 つにつき 1 行)が、ちょうど 10 個あることを確かめる(足りない・多いときは、手順が壊れているので、止める)。
pattern='name: ghcr\.io/[^,]+/v2/[a-z]+, newTag: '
count="$(grep -cE "$pattern" "$file" || true)"
if [[ "$count" != "10" ]]; then
  echo "newTag の行が 10 個ではありません(${count} 個): $file" >&2
  exit 1
fi

# 書き換える。
sed -Ei "s#(${pattern})[^}]+#\1${tag}#" "$file"
echo "newTag を ${tag} に更新しました(10 行): $file"

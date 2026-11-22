// GitHub Actions の workflow(イメージを作って ghcr.io に push し、launchs の webhook を呼んで、自動デプロイする)を、組み立てる。
// 作った YAML は、どのリポジトリに置いても動く(イメージの名前は、YAML の中で、そのリポジトリの持ち主から作る)。

/** ビルドの方法。dockerfile はフォルダの Dockerfile、railpack は Dockerfile なしで、コードから自動でイメージを作る。 */
export type WorkflowBuildMethod = "dockerfile" | "railpack"

/** workflow の設定。 */
export type WorkflowOptions = {
  /** 対象の deploy の ID(パッケージの名前に使う)。 */
  deployId: string
  /** ビルドの方法。 */
  build: WorkflowBuildMethod
  /** 起動するブランチ。 */
  branch: string
  /** ビルドの対象のフォルダ(リポジトリの直下からの相対。直下は ".")。 */
  folder: string
}

/**
 * GitHub のパッケージ(ghcr.io のイメージ)の名前を返す。
 * 他の deploy と名前が重ならないよう、deploy の ID から決める(deploy- の接頭辞は外す)。
 *
 * @param deployId deploy の ID。
 */
export function ghcrPackageName(deployId: string): string {
  return `launchs-${deployId.replace(/^deploy-/, "")}`
}

/** フォルダの入力を、workflow に書く形(先頭の ./ と末尾の / を外し、空なら ".")にする。 */
export function normalizeFolder(folder: string): string {
  const trimmed = folder.trim().replace(/^\.\//, "").replace(/\/+$/, "")
  return trimmed === "" ? "." : trimmed
}

/**
 * 自動デプロイの workflow(YAML)を作る。トークンと URL は、YAML に書かず、GitHub の Secrets(LAUNCHS_URL、LAUNCHS_TOKEN)から読む。
 *
 * @param options workflow の設定。
 */
export function buildGithubActionsWorkflow(options: WorkflowOptions): string {
  const packageName = ghcrPackageName(options.deployId)
  const folder = normalizeFolder(options.folder)
  const branch = options.branch.trim() || "main"
  // 画面を開いている、このサービスの URL(Secrets に入れる値の、例として書く)。
  const origin = typeof window === "undefined" ? "https://launchs.example.com" : window.location.origin
  const image = "${{ steps.image.outputs.name }}"
  const build = options.build === "railpack"
    ? `      # Railpack: Dockerfile が無くても、コードを見て、自動でイメージを作る。
      # (iloveitaly/github-action-railpack。@master は動く先が変わるので、慣れたら、コミットの ID に固定すると安全)
      - name: イメージを作って push する
        uses: iloveitaly/github-action-railpack@master
        with:
          context: ${folder}
          push: true
          # 毎回違うタグ(コミットの ID)にする。latest だけだと、変わったと判断できない。
          tags: ${image}:\${{ github.sha }}
`
    : `      - uses: docker/setup-buildx-action@v3
      - name: イメージを作って push する
        uses: docker/build-push-action@v6
        with:
          context: ${folder}
          push: true
          # 毎回違うタグ(コミットの ID)にする。latest だけだと、変わったと判断できない。
          tags: ${image}:\${{ github.sha }}
`
  return `# launchs に自動デプロイする workflow(ビルド: ${options.build === "railpack" ? "Railpack" : "Dockerfile"})。
# どのリポジトリに置いても動く(イメージの名前は、下で、そのリポジトリの持ち主から作る)。
#
# 使う前に:
#   1. リポジトリの Settings > Secrets and variables > Actions に、次の 2 つを登録する。
#        LAUNCHS_URL    ${origin}
#        LAUNCHS_TOKEN  この deploy の「自動デプロイ(webhook)」で発行したトークン
#   2. 最初の実行のあと、ghcr.io のパッケージ「${packageName}」を、公開(Public)にする。
#        (GitHub のパッケージは、最初は非公開。launchs は、非公開のイメージを取得できない)
name: Deploy to launchs

# 動くとき: ${branch} ブランチへの push(自動)と、Actions タブの「Run workflow」(手動)。
on:
  push:
    branches: [${branch}]
  workflow_dispatch:

permissions:
  contents: read
  packages: write

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: イメージの名前を決める
        id: image
        # ghcr.io は小文字だけ。deploy ごとに違う名前にして、他のものと重ならないようにする。
        run: echo "name=ghcr.io/\${GITHUB_REPOSITORY_OWNER,,}/${packageName}" >> "$GITHUB_OUTPUT"
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: \${{ github.actor }}
          password: \${{ secrets.GITHUB_TOKEN }}
${build}      - name: launchs に反映
        run: |
          # 失敗したとき、原因が分かるよう、HTTP の番号と、返事の中身を出す(401: トークン、403: アクセスの制限、409: 反映中)。
          status=$(curl -sS --max-time 30 -o response.txt -w "%{http_code}" -X POST "\${{ secrets.LAUNCHS_URL }}/hooks/deploy" \\
            -H "X-Launchs-Token: \${{ secrets.LAUNCHS_TOKEN }}" \\
            -d "{\\"imageRef\\":\\"${image}:\${{ github.sha }}\\"}")
          echo "HTTP $status"
          cat response.txt
          echo
          case "$status" in 2*) ;; *) exit 1 ;; esac
`
}

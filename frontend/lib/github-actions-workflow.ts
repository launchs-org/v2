// GitHub Actions の workflow(イメージを作って push し、launchs の webhook を呼んで、自動デプロイする)を、組み立てる。

/** イメージの参照から、タグ(と digest)を外した、リポジトリの部分を返す(例: ghcr.io/org/app:v1 → ghcr.io/org/app)。 */
export function imageRepositoryOf(imageRef: string): string {
  const withoutDigest = imageRef.split("@")[0]
  const lastSlash = withoutDigest.lastIndexOf("/")
  const colon = withoutDigest.lastIndexOf(":")
  // 最後の「/」より後ろの「:」が、タグ(ホストのポートと、取り違えない)。
  return colon > lastSlash ? withoutDigest.slice(0, colon) : withoutDigest
}

/**
 * 自動デプロイの workflow(YAML)を作る。トークンと URL は、YAML に書かず、GitHub の Secrets(LAUNCHS_URL、LAUNCHS_TOKEN)から読む。
 *
 * @param imageRef いまの deploy のイメージ(リポジトリの部分を、push 先に使う)。
 * @param branch 起動するブランチ。
 */
export function buildGithubActionsWorkflow(imageRef: string, branch = "main"): string {
  const repository = imageRepositoryOf(imageRef)
  const isGhcr = repository.startsWith("ghcr.io/")
  const registryHost = repository.split("/")[0]
  const login = isGhcr
    ? `      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: \${{ github.actor }}
          password: \${{ secrets.GITHUB_TOKEN }}
`
    : `      # ${registryHost} へのログインを、ここに足してください(docker/login-action)。
`
  return `# launchs に自動デプロイする workflow。
# 使う前に、リポジトリの Settings > Secrets and variables > Actions に、次の 2 つを登録してください。
#   LAUNCHS_URL    例: ${typeof window === "undefined" ? "https://launchs.example.com" : window.location.origin}
#   LAUNCHS_TOKEN  この deploy の「自動デプロイ(webhook)」で発行したトークン
${isGhcr ? "# 注意: ghcr.io のパッケージは、最初は非公開です。launchs は、非公開のイメージを取得できないので、\n#       GitHub の Packages の設定で、公開(Public)にしてください。\n" : ""}name: Deploy to launchs

on:
  push:
    branches: [${branch}]

permissions:
  contents: read
  packages: write

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
${login}      - uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          # 毎回違うタグ(コミットの ID)にする。latest だけだと、変わったと判断できない。
          tags: ${repository}:\${{ github.sha }}
      - name: launchs に反映
        run: |
          curl -fsS -X POST "\${{ secrets.LAUNCHS_URL }}/hooks/deploy" \\
            -H "X-Launchs-Token: \${{ secrets.LAUNCHS_TOKEN }}" \\
            -d "{\\"imageRef\\":\\"${repository}:\${{ github.sha }}\\"}"
`
}

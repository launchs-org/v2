// Monaco Editor の実行に要るファイルを、node_modules から public/monaco/vs に写す(CDN を使わず、自分のサーバーから配るため)。
// 要るのは、読み込み(loader.js)、エディタ本体(editor、base)、YAML の色分け(basic-languages/yaml)、日本語の文言(nls.messages.ja.js)。
// 重い言語サービス(language: TypeScript、JSON、CSS、HTML。約 7MB)と、他の言語の色分けは、写さない。
import { cpSync, existsSync, mkdirSync, rmSync } from "node:fs"
import { join } from "node:path"

const source = join(process.cwd(), "node_modules", "monaco-editor", "min", "vs")
const target = join(process.cwd(), "public", "monaco", "vs")

// monaco-editor が入っていないとき(依存を入れる前)は、何もしない。
if (!existsSync(source)) {
  console.warn("monaco-editor が見つからないので、写しませんでした")
  process.exit(0)
}
// 古い写しを消して、作り直す(版が変わったときに、古いファイルを残さない)。
rmSync(target, { recursive: true, force: true })
mkdirSync(target, { recursive: true })
for (const name of ["loader.js", "editor", "base", "nls.messages.ja.js"]) {
  cpSync(join(source, name), join(target, name), { recursive: true })
}
mkdirSync(join(target, "basic-languages", "yaml"), { recursive: true })
cpSync(join(source, "basic-languages", "yaml"), join(target, "basic-languages", "yaml"), { recursive: true })
console.log("Monaco Editor を public/monaco/vs に写しました")

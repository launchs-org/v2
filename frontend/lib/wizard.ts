// 最初のデプロイのウィザード用の、定数と、純粋関数。

/** よく使うイメージ(押すと入力欄に入る)。port は、そのイメージが、普通に待ち受けるポート。 */
export const POPULAR_IMAGES: { image: string; label: string; port: number }[] = [
  { image: "nginx:latest", label: "nginx", port: 80 },
  { image: "httpd:latest", label: "Apache", port: 80 },
  { image: "redis:7", label: "Redis", port: 6379 },
  { image: "postgres:16", label: "PostgreSQL", port: 5432 },
  { image: "node:22-alpine", label: "Node.js", port: 8080 },
  { image: "python:3.13-slim", label: "Python", port: 8080 },
]

/** 選べるスペックの、名前と、初心者向けの一言(CPU とメモリの値は、API が返さないので、書かない)。 */
export const SPEC_CHOICES: { name: "small" | "medium" | "large"; hint: string }[] = [
  { name: "small", hint: "迷ったら、これ。小さなアプリや、試すとき向け。" },
  { name: "medium", hint: "アクセスが増えたときや、少し重いアプリ向け。" },
  { name: "large", hint: "重い処理や、データベース向け。" },
]

/** イメージ名から、既定のポートを推測するための、(名前の一部, ポート)の表。上から順に、最初に合ったものを使う。 */
const PORT_HINTS: [string, number][] = [
  ["nginx", 80],
  ["httpd", 80],
  ["apache", 80],
  ["postgres", 5432],
  ["redis", 6379],
  ["mysql", 3306],
  ["mariadb", 3306],
  ["mongo", 27017],
]

/**
 * イメージ名から、既定のポートを推測する。
 *
 * @param image イメージの参照(例: `registry.example/team/postgres:16`)。
 * @returns 名前に、知っているものが含まれていれば、そのポート。なければ 8080。
 */
export function guessPort(image: string): number {
  // タグとレジストリの部分を除いた、イメージの名前(小文字)を取り出す。
  const lastSegment = image.trim().toLowerCase().split("/").pop() ?? ""
  const name = lastSegment.split(":")[0] ?? ""
  // 表を上から見て、名前に含まれるものを探す。
  for (const [hint, port] of PORT_HINTS) {
    if (name.includes(hint)) return port
  }
  // 知らないものは、よく使われる 8080 にする。
  return 8080
}

/**
 * イメージの入力を検査する。
 *
 * @param image 入力されたイメージ(前後の空白は、除いてから渡す)。
 * @returns 問題の説明。問題がなければ null。
 */
export function validateImage(image: string): string | null {
  // 空は、受け付けない。
  if (image.length === 0) return "イメージを入力してください。"
  // 空白を含むものは、イメージの参照として成り立たない。
  if (/\s/.test(image)) return "イメージに、空白は使えません。"
  // サーバーの上限(512 文字)を、手元でも確かめる。
  if (image.length > 512) return "イメージは、512 文字以内にしてください。"
  return null
}

/**
 * ポートの入力を検査する。
 *
 * @param text 入力された文字列。
 * @returns 問題の説明。問題がなければ null。
 */
export function validatePort(text: string): string | null {
  // 整数で、1〜65535 の範囲だけを認める。
  const port = Number(text)
  if (!/^\d+$/.test(text) || !Number.isInteger(port) || port < 1 || port > 65535) return "ポートは、1〜65535 の整数にしてください。"
  return null
}

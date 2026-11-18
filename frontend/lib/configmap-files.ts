// ConfigMap のファイル(docs/configmap.md、docs/endpoints/configmaps.md)の、手元での検査と、読み込み・書き出し。
// 上限と規則は、バックエンド(shared/model/configmap.go)と同じにする。サーバーも確かめるが、送る前に止めて、すぐ知らせる。
import type { ConfigMapFile } from "@/lib/types"

/** 保存する大きさの合計の上限(バイト)。ファイル 1 つの上限も、同じ。 */
export const MAX_STORED_BYTES = 1024 * 1024
/** ファイルの数の上限。 */
export const MAX_FILES = 50
/** ファイル名(key)の長さの上限。 */
export const MAX_KEY_LENGTH = 253
/** ConfigMap の名前、deploy のマウントの path の上限など、画面で使う規則。 */
export const MAX_MOUNTS = 30

const KEY_PATTERN = /^[A-Za-z0-9._-]+$/
const MODE_PATTERN = /^0[0-7]{3}$/
const FILE_PATH_PATTERN = /^\/[A-Za-z0-9._\-/]*$/

/** 画面で編集している、ファイル 1 つ(下書き)。size は元のバイト数(text は UTF-8 のバイト数、base64 はデコードした大きさ)。 */
export type DraftFile = ConfigMapFile & { size: number; /** 画面の中だけの識別(並び替えや編集で、行を保つため)。 */ uid: string }

let uidCounter = 0

/**
 * 下書きの行の、画面の中だけの識別子を作る。
 *
 * @returns 一意の文字列。
 */
export function newUid(): string {
  // 数を増やしていくだけで、一意になる。
  uidCounter += 1
  return `f${uidCounter}`
}

/**
 * 文字列の UTF-8 でのバイト数を数える。
 *
 * @param text 文字列。
 * @returns バイト数。
 */
export function utf8Length(text: string): number {
  // TextEncoder で、実際のバイト列にして数える。
  return new TextEncoder().encode(text).length
}

/**
 * ファイル 1 つの、保存する大きさ(key の長さ + 内容の長さ。base64 は、base64 の文字列の長さ)を返す。バックエンドの数え方と同じ。
 *
 * @param file ファイル。
 * @returns 保存する大きさ(バイト)。
 */
export function storedBytes(file: { key: string; content: string }): number {
  // key は ASCII だけ(検査で保証)。内容の text は、UTF-8 のバイト数で数える。
  return file.key.length + utf8Length(file.content)
}

/**
 * ファイルの組の、保存する大きさの合計を返す。
 *
 * @param files ファイル。
 * @returns 合計(バイト)。
 */
export function totalStoredBytes(files: { key: string; content: string }[]): number {
  // 1 つずつ足す。
  return files.reduce((sum, file) => sum + storedBytes(file), 0)
}

/**
 * バイト数を、読みやすい文字にする。
 *
 * @param bytes バイト数。
 * @returns 「512 B」「12.3 KiB」「1.00 MiB」の形。
 */
export function formatBytes(bytes: number): string {
  // 単位を、大きさで選ぶ。
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MiB`
}

/**
 * バイト列を、base64 の文字列にする(大きいデータでも、引数の数の上限に当たらないよう、分けて変換する)。
 *
 * @param bytes バイト列。
 * @returns 標準の base64(パディングあり)。
 */
export function bytesToBase64(bytes: Uint8Array): string {
  // 8 KiB ずつ、文字にして、つなぐ。
  let binary = ""
  for (let offset = 0; offset < bytes.length; offset += 0x2000) binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x2000))
  return btoa(binary)
}

/**
 * base64 の文字列を、バイト列にする。
 *
 * @param text 標準の base64。
 * @returns バイト列。
 */
export function base64ToBytes(text: string): Uint8Array {
  // 文字ごとに、バイトにする。
  const binary = atob(text)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index)
  return bytes
}

/**
 * 読み込んだバイト列から、ファイルの内容を決める。UTF-8 として読めて、NUL 文字を含まなければ text、そうでなければ base64。
 *
 * @param bytes ファイルのバイト列。
 * @returns encoding と、内容と、元のバイト数。
 */
export function entryFromBytes(bytes: Uint8Array): { encoding: "text" | "base64"; content: string; size: number } {
  // 厳密に UTF-8 として読む(壊れた並びがあれば、例外になる)。
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes)
    if (!text.includes("\u0000")) return { encoding: "text", content: text, size: bytes.length }
  } catch {
    // UTF-8 として読めないので、バイナリとして扱う。
  }
  return { encoding: "base64", content: bytesToBase64(bytes), size: bytes.length }
}

/**
 * ブラウザのファイルを読み込んで、下書きのファイルにする。key は、ファイル名。
 *
 * @param file ブラウザの File。
 * @returns 下書きのファイル(key が使えない文字を含むかは、検査で知らせる)。
 */
export async function draftFromFile(file: File): Promise<DraftFile> {
  // バイト列として読み、text か base64 かを決める。
  const entry = entryFromBytes(new Uint8Array(await file.arrayBuffer()))
  return { uid: newUid(), key: file.name, ...entry }
}

/**
 * 下書きのファイルを、ブラウザでダウンロードさせる。
 *
 * @param file ダウンロードするファイル。
 */
export function downloadDraftFile(file: ConfigMapFile) {
  // text は UTF-8 のバイト列に、base64 はデコードして、Blob にする。
  const bytes = file.encoding === "base64" ? base64ToBytes(file.content) : new TextEncoder().encode(file.content)
  const url = URL.createObjectURL(new Blob([bytes as BlobPart], { type: "application/octet-stream" }))
  const link = document.createElement("a")
  link.href = url
  link.download = file.key
  link.click()
  // 使い終わった URL は、すぐ解放する。
  URL.revokeObjectURL(url)
}

/**
 * ファイル名(key)を検査する。
 *
 * @param key 検査する key。
 * @returns 問題の説明。合っていれば空文字。
 */
export function validateKey(key: string): string {
  // 書式、長さ、「.」と「..」を確かめる。
  if (!key) return "ファイル名を入れてください。"
  if (key.length > MAX_KEY_LENGTH) return `ファイル名「${key.slice(0, 20)}…」が長すぎます(${MAX_KEY_LENGTH} 文字まで)。`
  if (key === "." || key === "..") return "ファイル名に「.」と「..」は使えません。"
  if (!KEY_PATTERN.test(key)) return `ファイル名「${key}」は、英数字と . _ - だけにしてください(「/」は使えません。階層は、マウント先のパスで作ります)。`
  return ""
}

/**
 * ファイルの組(下書き)を検査する。
 *
 * @param files 下書きのファイル。
 * @returns 問題の説明。合っていれば空文字。
 */
export function validateDraftFiles(files: DraftFile[]): string {
  // 個数、key、重複、1 つの大きさ、合計の大きさの順に確かめる。
  if (files.length > MAX_FILES) return `ファイルは ${MAX_FILES} 個までです。`
  const seen = new Set<string>()
  for (const file of files) {
    const keyProblem = validateKey(file.key)
    if (keyProblem) return keyProblem
    if (seen.has(file.key)) return `ファイル名「${file.key}」が重複しています。`
    seen.add(file.key)
    if (file.encoding === "text" && file.content.includes("\u0000")) return `「${file.key}」に NUL 文字が含まれています。バイナリは、アップロードで追加してください。`
    if (storedBytes(file) > MAX_STORED_BYTES) return `「${file.key}」が大きすぎます(${formatBytes(MAX_STORED_BYTES)} まで。バイナリは、base64 にした後の大きさで数えます)。`
  }
  if (totalStoredBytes(files) > MAX_STORED_BYTES) return `ファイルの大きさの合計が、上限(${formatBytes(MAX_STORED_BYTES)})を超えています。`
  return ""
}

/**
 * マウント先の path を検査する(deploy のファイルと同じ規則)。
 *
 * @param path 検査する path。
 * @returns 問題の説明。合っていれば空文字。
 */
export function validateMountPath(path: string): string {
  // 「/」で始まる、使える文字、「..」「//」、末尾の「/」、長さ。
  if (!FILE_PATH_PATTERN.test(path) || path === "/" || path.includes("..") || path.includes("//") || path.endsWith("/") || path.length > 200) {
    return `パス「${path}」は、「/」で始まる英数字と . _ - / のファイルのパスにしてください(「..」と末尾の「/」は不可。200 文字まで)。`
  }
  return ""
}

/**
 * 権限(mode)を検査する。
 *
 * @param mode 検査する mode。空は、既定(0644)なので許す。
 * @returns 問題の説明。合っていれば空文字。
 */
export function validateMode(mode: string): string {
  // 8 進数の 4 桁。
  if (mode !== "" && !MODE_PATTERN.test(mode)) return `権限「${mode}」は、8 進数の 4 桁(0000〜0777)にしてください。`
  return ""
}

/**
 * ファイルの組が、同じ中身かを返す(key の順序は問わない)。保存ボタンを出すかどうかの判断に使う。
 *
 * @param left 1 つ目。
 * @param right 2 つ目。
 * @returns 同じなら true。
 */
export function sameFiles(left: ConfigMapFile[], right: ConfigMapFile[]): boolean {
  // 個数が違えば違う。key で引いて、encoding と内容を比べる。
  if (left.length !== right.length) return false
  const rightByKey = new Map(right.map((file) => [file.key, file]))
  return left.every((file) => {
    const other = rightByKey.get(file.key)
    return other !== undefined && other.encoding === file.encoding && other.content === file.content
  })
}

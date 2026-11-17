/**
 * 日時(ISO 8601)を、画面に出す形(JST の「2026/11/16 16:32」)にする。読めない値は、そのまま返す。
 *
 * @param iso API が返した日時。
 * @returns 表示用の文字列。
 */
export function shortDateTime(iso: string): string {
  // 読めない値は、そのまま見せる。
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString("ja-JP", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", timeZone: "Asia/Tokyo" })
}

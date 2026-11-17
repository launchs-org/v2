// ログの行の、解析(時刻とレベルの取り出し、行への分割、検索語のハイライト)。画面に依存しない、純粋な関数だけを置く。

/** ログの行のレベル(色分けに使う)。 */
export type LogLevel = "error" | "warn" | "info" | "debug"

/** 解析した、ログの1行。 */
export type ParsedLine = {
  /** 行の先頭の時刻(ISO 8601)。読めなければ null。 */
  time: string | null
  /** 時刻を除いた本文。 */
  message: string
  level: LogLevel
}

/** 行の先頭の、k8s のログの時刻(例: 2026-11-16T16:32:36.168+09:00)を取り出す正規表現。 */
const TIME_PATTERN = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))\s?([\s\S]*)$/

/**
 * 取得したログの文字列を、行に分ける(空の行は捨てる)。
 *
 * 例: splitLogs("a\nb\n\n") → ["a", "b"]
 *
 * @param text API が返した、改行で区切られたログ。
 * @returns 空でない行の配列。
 */
export function splitLogs(text: string): string[] {
  // 改行で分けて、空の行(末尾の改行を含む)を除く。
  return text.split("\n").filter((line) => line.trim() !== "")
}

/**
 * 本文から、レベルを推定する。
 *
 * 例:
 *   "[error] 1#1: boom"           → error
 *   "level=warn msg=slow"         → warn
 *   "\"GET / HTTP/1.1\" 502 0"    → error(HTTP の 5xx)
 *   "\"GET / HTTP/1.1\" 404 0"    → warn(HTTP の 4xx)
 *   "[notice] start"              → info
 *
 * @param message 時刻を除いた本文。
 * @returns 推定したレベル。
 */
export function detectLevel(message: string): LogLevel {
  // 重い順に調べる: error を先に、次に warn、debug。
  if (/\b(ERROR|FATAL|PANIC|CRIT(?:ICAL)?)\b|\[(?:error|emerg|alert|crit)\]|level=(?:error|fatal)/i.test(message)) return "error"
  // HTTP のアクセスログ(" 5xx )は、error、(" 4xx )は warn。
  const status = /"\s+([45]\d{2})\s/.exec(message)
  if (status?.[1]?.startsWith("5")) return "error"
  if (/\bWARN(?:ING)?\b|\[warn\]|level=warn/i.test(message)) return "warn"
  if (status?.[1]?.startsWith("4")) return "warn"
  if (/\b(DEBUG|TRACE)\b|\[(?:debug|trace)\]|level=(?:debug|trace)/i.test(message)) return "debug"
  return "info"
}

/**
 * ログの1行を、時刻・本文・レベルに分ける。
 *
 * 例: parseLine("2026-11-16T16:32:36.168+09:00 [error] boom")
 *   → { time: "2026-11-16T16:32:36.168+09:00", message: "[error] boom", level: "error" }
 *
 * @param raw 生の1行(先頭に、時刻が付いていることが多い)。
 * @returns 解析した行。時刻が読めなければ、time は null で、message は行全体。
 */
export function parseLine(raw: string): ParsedLine {
  // 先頭の時刻を、取り出す。
  const matched = TIME_PATTERN.exec(raw)
  const time = matched?.[1] ?? null
  const message = matched ? (matched[2] ?? "") : raw
  return { time, message, level: detectLevel(message) }
}

/**
 * 表示用に、時刻を「時:分:秒.ミリ秒」にする(日付は省く)。
 *
 * 例: shortTime("2026-11-16T16:32:36.168+09:00") → "16:32:36.168"
 *
 * @param time ISO 8601 の時刻。
 * @returns 表示用の文字列。読めなければ、そのまま。
 */
export function shortTime(time: string): string {
  // 「T」と、タイムゾーンの間を、取り出す(タイムゾーンは、API が JST に揃えている)。
  const matched = /T(\d{2}:\d{2}:\d{2}(?:\.\d+)?)/.exec(time)
  return matched?.[1] ?? time
}

/**
 * 本文を、検索語に一致する部分と、しない部分に分ける(大文字小文字は区別しない)。
 *
 * 例: highlight("Start Worker", "work") → [{ text: "Start ", hit: false }, { text: "Work", hit: true }, { text: "er", hit: false }]
 *
 * @param message 本文。
 * @param query 検索語。空なら、分けない。
 * @returns 一致の有無つきの、断片の配列。つなげると、元の本文に戻る。
 */
export function highlight(message: string, query: string): { text: string; hit: boolean }[] {
  // 検索語が空なら、全体を、1つの断片にする。
  if (query === "") return [{ text: message, hit: false }]
  const lowerMessage = message.toLowerCase()
  const lowerQuery = query.toLowerCase()
  const parts: { text: string; hit: boolean }[] = []
  let cursor = 0
  // 一致する位置を、順に探して、前の部分と、一致の部分に分けていく。
  for (let index = lowerMessage.indexOf(lowerQuery); index !== -1; index = lowerMessage.indexOf(lowerQuery, cursor)) {
    if (index > cursor) parts.push({ text: message.slice(cursor, index), hit: false })
    parts.push({ text: message.slice(index, index + query.length), hit: true })
    cursor = index + query.length
  }
  // 最後の一致の後ろを、足す。
  if (cursor < message.length) parts.push({ text: message.slice(cursor), hit: false })
  return parts
}

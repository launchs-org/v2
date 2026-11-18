"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import type { AccessLogEntry, AccessLogsResponse } from "@/lib/types"
import { formatBytes } from "@/lib/configmap-files"

/** 先頭のページを取り直す間隔(ミリ秒)。新しいログが、数秒で出る。 */
const LIVE_INTERVAL_MS = 5000

/** 1 ページの件数。 */
const PAGE_SIZE = 100

const STATUS_FILTERS = [
  { value: "", label: "全てのステータス" },
  { value: "2xx", label: "2xx" },
  { value: "3xx", label: "3xx" },
  { value: "4xx", label: "4xx" },
  { value: "5xx", label: "5xx" },
]
const METHOD_FILTERS = ["", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]

/**
 * ステータスの色(2xx は通常、3xx は控えめ、4xx は注意、5xx は警告)。
 *
 * @param status ステータス(0 は、記録できなかったもの)。
 * @returns CSS の色。
 */
function statusColor(status: number): string {
  // 百の位で決める。
  if (status >= 500) return "var(--bad)"
  if (status >= 400) return "var(--warn, #b7791f)"
  if (status >= 300) return "var(--mut)"
  return "inherit"
}

/**
 * 時刻を、「時:分:秒.ミリ秒」の形にする(同じ日のログを見るため、日付は省く。日付は、ツールチップに出す)。
 *
 * @param iso ISO 8601 の時刻。
 * @returns 表示用の文字列。
 */
function formatTime(iso: string): string {
  // ローカル時刻で、ミリ秒まで出す。
  const date = new Date(iso)
  return `${date.toLocaleTimeString("ja-JP", { hour12: false })}.${String(date.getMilliseconds()).padStart(3, "0")}`
}

/**
 * ingress のアクセスログ(1 リクエストごとの記録)を、新しい順の表で表示する。URL は、クエリも含めて、そのまま。
 *
 * 先頭のページは、数秒ごとに取り直す。「さらに読み込む」で、古いログを続けて読む。
 *
 * @param props.basePath ingress の API のパス(/api/v1/projects/.../ingresses/...)。
 */
export function IngressAccessLog({ basePath }: { basePath: string }) {
  const [status, setStatus] = useState("")
  const [method, setMethod] = useState("")
  const [clientIp, setClientIp] = useState("")
  const [text, setText] = useState("")
  // 入力中の文字は、確定(Enter か、フォーカスが外れる)してから、絞り込みに使う。
  const [applied, setApplied] = useState({ ip: "", q: "" })
  const [older, setOlder] = useState<AccessLogEntry[]>([])
  const [olderCursor, setOlderCursor] = useState<string | null | undefined>(undefined)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState("")

  // 絞り込みの条件を、クエリ文字列にする。
  const params = new URLSearchParams({ limit: String(PAGE_SIZE) })
  if (status) params.set("status", status)
  if (method) params.set("method", method)
  if (applied.ip) params.set("ip", applied.ip)
  if (applied.q) params.set("q", applied.q)
  const filterKey = params.toString()
  const { data, error, isValidating } = useSWR<AccessLogsResponse>(`${basePath}/access-logs?${filterKey}`, authenticatedFetcher, { refreshInterval: LIVE_INTERVAL_MS, revalidateOnFocus: true })

  // 絞り込みが変わったら、続きのページは捨てる。
  useEffect(() => { setOlder([]); setOlderCursor(undefined); setMoreError("") }, [filterKey])

  // 先頭のページの、次のページの位置(続きを読んだあとは、読んだ最後の位置)。
  const nextCursor = olderCursor === undefined ? data?.nextCursor ?? null : olderCursor
  // 先頭のページが取り直されて、先頭に新しいログが増えても、続きのページと重ならないよう、時刻と URL で重複を除く。
  const seen = new Set<string>()
  const rows = [...(data?.logs ?? []), ...older].filter((entry) => {
    const key = `${entry.time}|${entry.method}|${entry.url}|${entry.clientIp}|${entry.bytes}|${entry.status}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })

  /** 古いログを、もう 1 ページ読み込む。 */
  async function loadMore() {
    if (!nextCursor) return
    setLoadingMore(true)
    setMoreError("")
    try {
      const page = await authenticatedFetcher<AccessLogsResponse>(`${basePath}/access-logs?${filterKey}&cursor=${encodeURIComponent(nextCursor)}`)
      setOlder((current) => [...current, ...page.logs])
      setOlderCursor(page.nextCursor)
    } catch (caught) {
      setMoreError(caught instanceof Error ? caught.message : "読み込めませんでした")
    } finally {
      setLoadingMore(false)
    }
  }

  /** IP と URL の文字列を、絞り込みに反映する。 */
  function applyText() { setApplied({ ip: clientIp.trim(), q: text.trim() }) }

  return (
    <div style={{ borderTop: "1px solid var(--line2)" }}>
      <div className="row pad" style={{ paddingBottom: 8 }}>
        <h3 className="h2 grow" style={{ fontSize: 13 }}>リクエスト一覧(新しい順)</h3>
        <span className="faint" style={{ fontSize: 12 }}>{isValidating ? "更新中…" : `${LIVE_INTERVAL_MS / 1000} 秒ごとに更新`}</span>
      </div>
      <div className="row" style={{ gap: 8, padding: "0 18px 10px", flexWrap: "wrap" }}>
        <select className="inp" value={status} onChange={(event) => setStatus(event.target.value)} aria-label="ステータス">{STATUS_FILTERS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select>
        <select className="inp" value={method} onChange={(event) => setMethod(event.target.value)} aria-label="メソッド">{METHOD_FILTERS.map((item) => <option key={item} value={item}>{item || "全てのメソッド"}</option>)}</select>
        <input className="inp mono" style={{ width: 150 }} placeholder="IP アドレス" value={clientIp} onChange={(event) => setClientIp(event.target.value)} onBlur={applyText} onKeyDown={(event) => { if (event.key === "Enter") applyText() }} aria-label="IP アドレス" />
        <input className="inp mono" style={{ flex: 1, minWidth: 160 }} placeholder="URL に含まれる文字(クエリも)" value={text} onChange={(event) => setText(event.target.value)} onBlur={applyText} onKeyDown={(event) => { if (event.key === "Enter") applyText() }} aria-label="URL" />
      </div>
      {error && <div style={{ color: "var(--bad)", fontSize: 12.5, padding: "0 18px 12px" }}>アクセスログを取得できませんでした。</div>}
      <div style={{ overflowX: "auto" }}>
        <table className="mono" style={{ width: "100%", borderCollapse: "collapse", fontSize: 12.5, whiteSpace: "nowrap" }}>
          <thead>
            <tr style={{ textAlign: "left", color: "var(--mut)" }}>
              {["時刻", "メソッド", "URL", "ステータス", "時間", "IP", "サイズ"].map((label) => <th key={label} style={{ padding: "6px 10px", fontWeight: 500, borderBottom: "1px solid var(--line2)" }}>{label}</th>)}
            </tr>
          </thead>
          <tbody>
            {rows.map((entry, index) => (
              <tr key={`${entry.time}-${index}`} style={{ borderBottom: "1px solid var(--line2)" }}>
                <td style={{ padding: "5px 10px" }} title={new Date(entry.time).toLocaleString("ja-JP")}>{formatTime(entry.time)}</td>
                <td style={{ padding: "5px 10px" }}>{entry.method}</td>
                <td style={{ padding: "5px 10px", maxWidth: 420, overflow: "hidden", textOverflow: "ellipsis" }} title={entry.url}>{entry.url}</td>
                <td style={{ padding: "5px 10px", color: statusColor(entry.status) }}>{entry.status || "—"}</td>
                <td style={{ padding: "5px 10px" }}>{entry.durationMs.toFixed(1)} ms</td>
                <td style={{ padding: "5px 10px" }}>{entry.clientIp || "—"}</td>
                <td style={{ padding: "5px 10px" }}>{formatBytes(entry.bytes)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {data && rows.length === 0 && <div className="mut" style={{ fontSize: 12.5, padding: "12px 18px" }}>該当するアクセスログがありません(記録を有効にしたあとのアクセスだけが残ります)。</div>}
      {(nextCursor || moreError) && (
        <div className="row" style={{ padding: "10px 18px 14px", gap: 10 }}>
          {nextCursor && <button className="btn sm" type="button" disabled={loadingMore} onClick={() => void loadMore()}>{loadingMore ? "読み込み中…" : "さらに読み込む"}</button>}
          {moreError && <span style={{ color: "var(--bad)", fontSize: 12.5 }}>{moreError}</span>}
        </div>
      )}
    </div>
  )
}

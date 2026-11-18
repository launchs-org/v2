"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
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
 * 日時を、「月/日 時:分:秒.ミリ秒」の形にする。
 *
 * @param iso ISO 8601 の時刻。
 * @returns 表示用の文字列。
 */
function formatTime(iso: string): string {
  // ローカル時刻で、ミリ秒まで出す。
  const date = new Date(iso)
  return `${date.toLocaleDateString("ja-JP", { month: "2-digit", day: "2-digit" })} ${date.toLocaleTimeString("ja-JP", { hour12: false })}.${String(date.getMilliseconds()).padStart(3, "0")}`
}

/**
 * アクセスログの記録の有効・無効を、API に送る。
 *
 * @param basePath ingress の API のパス。
 * @param next 有効にするなら true。
 */
export async function updateAccessLogEnabled(basePath: string, next: boolean) {
  // PATCH で切り替える(サーバーが、IngressRoute の作り直しを始める)。
  await api.patch(basePath, { accessStats: next })
}

/**
 * datetime-local の入力値(例: 2026-11-18T10:00)を、API に送る RFC 3339 にする(ローカル時刻として読む)。
 *
 * @param value 入力値。空なら空。
 * @returns RFC 3339(UTC)。読めなければ空。
 */
function toIso(value: string): string {
  // 空や不正な値は、絞り込まない。
  const date = new Date(value)
  return value && !Number.isNaN(date.getTime()) ? date.toISOString() : ""
}

/**
 * ingress のアクセスログ(1 リクエストごとの記録)を、新しい順の表で表示する。URL は、クエリも含めて、そのまま。
 *
 * 日時、パス(URL に含まれる文字)、IP、ステータス、メソッドで絞り込める。更新ボタンで取り直し、自動更新(5 秒ごと)も選べる。
 * 「さらに読み込む」で、古いログを続けて読む。
 *
 * @param props.basePath ingress の API のパス(/api/v1/projects/.../ingresses/...)。
 * @param props.enabled 記録が有効か。
 * @param props.locked 反映中・削除中(切り替えられない)。
 * @param props.onToggle 有効・無効を切り替えるとき。
 */
export function IngressAccessLog({ basePath, enabled, locked, onToggle }: { basePath: string; enabled: boolean; locked: boolean; onToggle: (next: boolean) => void }) {
  const [status, setStatus] = useState("")
  const [method, setMethod] = useState("")
  const [clientIp, setClientIp] = useState("")
  const [text, setText] = useState("")
  const [from, setFrom] = useState("")
  const [to, setTo] = useState("")
  const [auto, setAuto] = useState(true)
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
  if (toIso(from)) params.set("from", toIso(from))
  if (toIso(to)) params.set("to", toIso(to))
  const filterKey = params.toString()
  const { data, error, isValidating, mutate } = useSWR<AccessLogsResponse>(`${basePath}/access-logs?${filterKey}`, authenticatedFetcher, { refreshInterval: auto ? LIVE_INTERVAL_MS : 0, revalidateOnFocus: true })

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
    <div>
      <div className="row pad" style={{ paddingBottom: 8, gap: 10, flexWrap: "wrap" }}>
        <h2 className="h2 grow">アクセスログ</h2>
        <label className="row" style={{ gap: 6, fontSize: 12.5 }}><input type="checkbox" checked={auto} onChange={(event) => setAuto(event.target.checked)} />自動更新(5 秒)</label>
        <button className="btn sm" type="button" disabled={isValidating} onClick={() => { setOlder([]); setOlderCursor(undefined); void mutate() }}>{isValidating ? "更新中…" : "更新"}</button>
        <label className="row" style={{ gap: 6, fontSize: 12.5 }}><input type="checkbox" checked={enabled} disabled={locked} onChange={(event) => onToggle(event.target.checked)} />記録する</label>
      </div>
      {!enabled && <div className="mut" style={{ fontSize: 12.5, padding: "0 18px 8px" }}>記録は止まっています。過去の分は、保存期間の間、見られます。</div>}
      <div className="row" style={{ gap: 8, padding: "0 18px 10px", flexWrap: "wrap" }}>
        <input className="inp" type="datetime-local" step="1" value={from} onChange={(event) => setFrom(event.target.value)} aria-label="開始の日時" title="開始の日時" />
        <span className="mut">〜</span>
        <input className="inp" type="datetime-local" step="1" value={to} onChange={(event) => setTo(event.target.value)} aria-label="終了の日時" title="終了の日時" />
        <input className="inp mono" style={{ flex: 1, minWidth: 160 }} placeholder="パス(URL に含まれる文字。クエリも)" value={text} onChange={(event) => setText(event.target.value)} onBlur={applyText} onKeyDown={(event) => { if (event.key === "Enter") applyText() }} aria-label="パス" />
        <input className="inp mono" style={{ width: 150 }} placeholder="IP アドレス" value={clientIp} onChange={(event) => setClientIp(event.target.value)} onBlur={applyText} onKeyDown={(event) => { if (event.key === "Enter") applyText() }} aria-label="IP アドレス" />
        <select className="inp" value={status} onChange={(event) => setStatus(event.target.value)} aria-label="ステータス">{STATUS_FILTERS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select>
        <select className="inp" value={method} onChange={(event) => setMethod(event.target.value)} aria-label="メソッド">{METHOD_FILTERS.map((item) => <option key={item} value={item}>{item || "全てのメソッド"}</option>)}</select>
      </div>
      {error && <div style={{ color: "var(--bad)", fontSize: 12.5, padding: "0 18px 12px" }}>アクセスログを取得できませんでした。</div>}
      <div style={{ overflowX: "auto" }}>
        <table className="mono" style={{ width: "100%", borderCollapse: "collapse", fontSize: 12.5, whiteSpace: "nowrap" }}>
          <thead>
            <tr style={{ textAlign: "left", color: "var(--mut)" }}>
              {["日時", "メソッド", "パス", "ステータス", "時間", "IP", "サイズ"].map((label) => <th key={label} style={{ padding: "6px 10px", fontWeight: 500, borderBottom: "1px solid var(--line2)" }}>{label}</th>)}
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

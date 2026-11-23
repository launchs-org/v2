"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import { ErrorState, LoadingState } from "@/components/ui-kit"

type AccessLog = { time: string; host: string; ingressId: string; method: string; url: string; status: number; durationMs: number; clientIp: string; bytes: number }
type AccessLogList = { logs: AccessLog[]; nextCursor: string | null }

/** 行の列の幅。 */
const COLUMNS = "150px 1.2fr 70px 2fr 70px 90px 140px"

/**
 * 全 ingress のアクセスログ(読むだけ)。ステータスの分類(2xx〜5xx)、IP、キーワード(ホスト名、URL)で絞り込める。
 * 不正アクセスの確認に使う(ある IP の動きを、IP の欄の絞り込みで追える)。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function AdminAccessLogs({ refreshInterval }: { refreshInterval: number }) {
  const [status, setStatus] = useState("")
  const [clientIp, setClientIp] = useState("")
  const [keyword, setKeyword] = useState("")
  const query = `limit=100${status ? `&status=${status}` : ""}${clientIp.trim() ? `&ip=${encodeURIComponent(clientIp.trim())}` : ""}${keyword.trim() ? `&q=${encodeURIComponent(keyword.trim())}` : ""}`
  const { data, error, mutate } = useSWR<AccessLogList>(`/admin-api/access-logs?${query}`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  // 「もっと見る」で足したページ。
  const [more, setMore] = useState<AccessLog[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor

  /** 絞り込みを変えたとき、足したページを捨てる。 */
  function resetPaging() {
    setMore([])
    setCursor(undefined)
  }

  /** 次のページを読み足す。 */
  async function loadMore() {
    if (!nextCursor) return
    const page = await api.get<AccessLogList>(`/admin-api/access-logs?${query}&cursor=${encodeURIComponent(nextCursor)}`)
    setMore((current) => [...current, ...page.logs])
    setCursor(page.nextCursor)
  }

  const logs = data ? [...data.logs, ...more] : []
  const cols = { "--cols": COLUMNS } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 980 }}>
      <div className="pad row" style={{ paddingBottom: 8, gap: 10, flexWrap: "wrap" }}>
        <h2 className="h2 grow">アクセスログ(全体)</h2>
        <select className="inp" style={{ width: "auto" }} value={status} onChange={(event) => { setStatus(event.target.value); resetPaging() }} aria-label="ステータスで絞り込む">
          <option value="">すべて</option><option value="2xx">2xx</option><option value="3xx">3xx</option><option value="4xx">4xx</option><option value="5xx">5xx</option>
        </select>
        <input className="inp" style={{ width: 160 }} value={clientIp} onChange={(event) => { setClientIp(event.target.value); resetPaging() }} placeholder="IP" aria-label="IP で絞り込む" />
        <input className="inp" style={{ width: 200 }} value={keyword} onChange={(event) => { setKeyword(event.target.value); resetPaging() }} placeholder="ホスト名・URL" aria-label="キーワードで絞り込む" />
      </div>
      <div className="tr th" style={cols}><span>日時</span><span>ホスト</span><span>メソッド</span><span>URL</span><span>状態</span><span>時間</span><span>IP</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="アクセスログを取得できませんでした" onRetry={() => void mutate()} /></div>}
      {data && logs.length === 0 && <div className="pad mut">アクセスログはありません。</div>}
      {logs.map((log, index) => (
        <div key={`${log.time}-${index}`} className="tr" style={cols}>
          <span className="mono faint">{shortDateTime(log.time)}</span>
          <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>{log.host || <span className="faint">(削除済み)</span>}</span>
          <span>{log.method}</span>
          <span className="mono" style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={log.url}>{log.url}</span>
          <span className={`badge ${log.status >= 500 ? "b-bad" : log.status >= 400 ? "b-warn" : "b-ok"}`}><i />{log.status || "-"}</span>
          <span className="mono">{Math.round(log.durationMs)} ms</span>
          <span className="mono">{log.clientIp}</span>
        </div>
      ))}
      {nextCursor && <div className="pad"><button className="btn" type="button" onClick={() => void loadMore()}>もっと見る</button></div>}
    </div></div>
  )
}

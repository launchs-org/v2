"use client"

import { useEffect, useRef, useState } from "react"
import { api } from "@/lib/api"
import { detectLevel, highlight } from "@/lib/log-parse"
import type { DeployLogsResponse, LogPodInfo } from "@/lib/types"

/** 画面に持つ、ログの最大の行数。超えたら、古い行から捨てる。 */
const MAX_LINES = 50_000

/** 新しい行が無いとき、次に取るまでの待ち(ミリ秒)。 */
const IDLE_WAIT_MS = 2000

type LogTailProps = {
  deployId: string
  pods: LogPodInfo[]
  /** 選ばれている Pod の ID(上のチップで選ぶ)。 */
  podId: string
  /** 自動更新の間隔(ミリ秒)。0 なら、自動では取らない。 */
  refreshInterval: number
}

/**
 * 1つの Pod のログを、続きから取り続けて、端末の形で出す。
 * Pod かタブが変わる(アンマウントされる)と、取得を止める。LIVE の切り替えと、クリアのボタンを持つ。
 *
 * @param props.deployId deploy の ID。
 * @param props.pods ログのある Pod(稼働中かどうかの判定に使う)。
 * @param props.podId 選ばれている Pod の ID。
 * @param props.refreshInterval 0 なら、自動では取らない。
 */
export function LogTail({ deployId, pods, podId, refreshInterval }: LogTailProps) {
  const [lines, setLines] = useState<string[]>([])
  const [live, setLive] = useState(true)
  const [error, setError] = useState("")
  // この Pod のログの中での、検索(一致を強調。「一致する行だけ」で絞る)。
  const [query, setQuery] = useState("")
  const [onlyMatches, setOnlyMatches] = useState(false)
  const outputRef = useRef<HTMLDivElement>(null)
  const running = pods.find((pod) => pod.podId === podId)?.running ?? false
  const polling = live && refreshInterval > 0

  // Pod か、LIVE の状態が変わったら、取得のループを作り直す(古いループは、止める)。
  useEffect(() => {
    if (!podId) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let since: number | undefined
    /** 続きを1回取り、まだあれば直ぐに、無ければ待ってから、もう1回取る(稼働中のときだけ)。 */
    async function pull() {
      try {
        const path = `/api/v1/deploy/${encodeURIComponent(deployId)}/logs?podId=${encodeURIComponent(podId)}${since === undefined ? "" : `&since=${since}`}`
        const chunk = await api.get<DeployLogsResponse>(path)
        if (stopped) return
        since = chunk.nextSince
        setError("")
        if (chunk.logs.length > 0) {
          // 行に分けて、末尾に足す。多すぎたら、古い方を捨てる。
          const added = chunk.logs.split("\n").filter((line) => line !== "")
          setLines((current) => [...current, ...added].slice(-MAX_LINES))
          void pull()
          return
        }
        if (polling && running) timer = setTimeout(() => void pull(), IDLE_WAIT_MS)
      } catch (caught) {
        if (stopped) return
        setError(caught instanceof Error ? caught.message : "ログを取得できませんでした")
        if (polling && running) timer = setTimeout(() => void pull(), IDLE_WAIT_MS * 2)
      }
    }
    void pull()
    // 片付け: 取得を止め、待っている次の取得を取り消す。
    return () => {
      stopped = true
      if (timer) clearTimeout(timer)
    }
  }, [deployId, podId, polling, running])

  // Pod を変えたら、出力を空にする。
  useEffect(() => { setLines([]) }, [podId])

  // 行が増えたら、末尾が見えるようにスクロールする。
  useEffect(() => {
    if (outputRef.current) outputRef.current.scrollTop = outputRef.current.scrollHeight
  }, [lines])

  // 検索の文字に合う行だけにする(絞り込みがオンのとき)。大文字小文字は、区別しない。
  const needle = query.trim()
  const shown = onlyMatches && needle ? lines.filter((line) => line.toLowerCase().includes(needle.toLowerCase())) : lines
  return (
    <div className="logtail">
      <div className="row" style={{ gap: 8 }}>
        <label className="srch" style={{ flex: 1, width: "auto", height: 38 }}>
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="M20 20l-4-4" /></svg>
          <input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="この Pod のログを検索" aria-label="この Pod のログを検索" />
        </label>
        <label className="row" style={{ gap: 6, fontSize: 13, color: "var(--mut)", cursor: "pointer", whiteSpace: "nowrap" }}><input type="checkbox" checked={onlyMatches} onChange={(event) => setOnlyMatches(event.target.checked)} />一致する行だけ</label>
      </div>
      <div className="term" role="log" aria-label="デプロイログ" ref={outputRef}>
        {lines.length === 0 ? <div className="l-dbg">{podId ? "ログを待機しています…" : "ログのある Pod がありません"}</div> : shown.length === 0 ? <div className="l-dbg">「{needle}」に合う行がありません</div> : shown.map((line, index) => {
          // 行のレベルで、色を決める(ERROR は赤、WARN は橙)。検索に合う部分は、黄色で強調する。
          const level = detectLevel(line)
          const levelClass = level === "error" ? "l-err" : level === "warn" ? "l-warn" : level === "debug" ? "l-dbg" : "l-info"
          return <div key={index} className={levelClass}>{highlight(line, needle).map((part, partIndex) => part.hit ? <mark key={partIndex} className="log-hit">{part.text}</mark> : <span key={partIndex}>{part.text}</span>)}</div>
        })}
      </div>
      {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{error}</div>}
      <div className="row">
        <span className={`badge ${polling && running ? "b-ok live" : "b-dim"}`}><i />{polling && running ? "LIVE" : "PAUSED"}</span>
        <span className="faint mono" style={{ fontSize: 12 }}>{needle && onlyMatches ? `${shown.length.toLocaleString()} / ${lines.length.toLocaleString()}` : lines.length.toLocaleString()} 行</span>
        <div className="grow" />
        <button className="btn sm" type="button" onClick={() => setLive((current) => !current)}>{live ? "一時停止" : "再開"}</button>
        <button className="btn sm" type="button" onClick={() => setLines([])}>クリア</button>
      </div>
    </div>
  )
}

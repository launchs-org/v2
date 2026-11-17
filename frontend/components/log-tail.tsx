"use client"

import { useEffect, useRef, useState } from "react"
import { api } from "@/lib/api"
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

  return (
    <>
      <div className="term" role="log" aria-label="デプロイログ" ref={outputRef} style={{ height: "calc(100vh - 430px)", minHeight: 240 }}>
        {lines.length === 0 ? <div className="l-dbg">{podId ? "ログを待機しています…" : "ログのある Pod がありません"}</div> : lines.map((line, index) => <div key={index} className="l-info">{line}</div>)}
      </div>
      {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{error}</div>}
      <div className="row">
        <span className={`badge ${polling && running ? "b-ok live" : "b-dim"}`}><i />{polling && running ? "LIVE" : "PAUSED"}</span>
        <span className="faint mono" style={{ fontSize: 12 }}>{lines.length.toLocaleString()} 行</span>
        <div className="grow" />
        <button className="btn sm" type="button" onClick={() => setLive((current) => !current)}>{live ? "一時停止" : "再開"}</button>
        <button className="btn sm" type="button" onClick={() => setLines([])}>クリア</button>
      </div>
    </>
  )
}

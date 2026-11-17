"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { api } from "@/lib/api"
import { highlight, parseLine, shortTime, splitLogs, type LogLevel } from "@/lib/log-parse"
import type { DeployLogsResponse, LogPodInfo } from "@/lib/types"

/** 画面に持つ、ログの最大の行数。超えたら、古い行から捨てる。 */
const MAX_LINES = 50_000

/** 新しい行が無いとき、次に取るまでの最短の待ち(ミリ秒)。 */
const IDLE_WAIT_MS = 2000

/** 末尾にいるとみなす、スクロールの余裕(px)。 */
const BOTTOM_SLACK = 24

/** Pod の印に使う色(順に割り当てる)。 */
const POD_COLORS = ["#6D4AFF", "#0EA5E9", "#10B981", "#F59E0B", "#EC4899", "#14B8A6", "#8B5CF6", "#EF4444"]

/** 画面に持つ、ログの1行。 */
type Entry = {
  /** 一意の番号(React の key と、並びの安定に使う)。 */
  id: number
  podId: string
  raw: string
  time: string | null
  /** 並べ替え用の時刻(ミリ秒)。読めない行は、直前の行の値を引き継ぐ。 */
  sortKey: number
  message: string
  level: LogLevel
}

/** ログの表示の設定(上のツールバーで決める)。 */
export type LogViewOptions = {
  query: string
  onlyMatches: boolean
  levels: Set<LogLevel>
  showTime: boolean
  wrap: boolean
  paused: boolean
}

/** 親(ツールバー)が、読む・操作するための、取っ手。 */
export type LogViewerHandle = {
  clear: () => void
  download: () => void
}

type LogViewerProps = {
  deployId: string
  /** 取得する Pod(「すべて」なら、全部。個別なら、1つ)。 */
  pods: LogPodInfo[]
  options: LogViewOptions
  /** 自動更新の間隔(ミリ秒)。0 なら、自動では取らない。 */
  refreshInterval: number
  /** 親に、操作の取っ手と、行数を、渡す。 */
  onReady: (handle: LogViewerHandle) => void
  onCount: (shown: number, total: number) => void
}

/**
 * 選んだ Pod のログを、続きから取り続けて、時刻順に混ぜて見せる。
 * 検索のハイライト、レベルの色分け、自動スクロール(末尾にいるときだけ追従)を持つ。
 * deploy か Pod の集合が変わると、取得をやり直す。
 *
 * @param props.deployId deploy の ID。
 * @param props.pods 取得する Pod。
 * @param props.options 表示の設定。
 * @param props.refreshInterval 0 なら、自動では取らない。
 * @param props.onReady 操作の取っ手を、親に渡す。
 * @param props.onCount 表示中の行数と、全体の行数を、親に伝える。
 */
export function LogViewer({ deployId, pods, options, refreshInterval, onReady, onCount }: LogViewerProps) {
  const [entries, setEntries] = useState<Entry[]>([])
  const [error, setError] = useState("")
  const [atBottom, setAtBottom] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)
  const nextId = useRef(0)
  const lastSortKey = useRef(0)
  // Pod の ID の並び(集合が変わったかの判定に使う)。
  const podKey = pods.map((pod) => pod.podId).join(",")
  const runningKey = pods.map((pod) => `${pod.podId}:${pod.running ? 1 : 0}`).join(",")
  const polling = !options.paused && refreshInterval > 0

  // Pod ごとの色(Pod の順に、色を割り当てる)。
  const colors = useMemo(() => new Map(pods.map((pod, index) => [pod.podId, POD_COLORS[index % POD_COLORS.length] ?? "#6D4AFF"])), [pods])

  // deploy か Pod の集合が変わったら、出力を空にして、取り直す。
  useEffect(() => {
    setEntries([])
    nextId.current = 0
    lastSortKey.current = 0
  }, [deployId, podKey])

  // 取得のループ: Pod ごとに、続きを取り続ける。設定や Pod が変わったら、古いループを止めて、作り直す。
  useEffect(() => {
    if (!pods.length) return
    let stopped = false
    const timers = new Map<string, ReturnType<typeof setTimeout>>()
    const since = new Map<string, number>()

    /** 取れた分の文字列を、解析して、時刻順に混ぜて、末尾に足す。 */
    function append(podId: string, text: string) {
      // 行に分けて、1行ずつ解析する(時刻が読めない行は、直前の時刻を引き継ぐ)。
      const added: Entry[] = []
      for (const raw of splitLogs(text)) {
        const parsed = parseLine(raw)
        const parsedTime = parsed.time === null ? NaN : Date.parse(parsed.time)
        const sortKey = Number.isNaN(parsedTime) ? lastSortKey.current : parsedTime
        lastSortKey.current = Math.max(lastSortKey.current, sortKey)
        added.push({ id: nextId.current++, podId, raw, time: parsed.time, sortKey, message: parsed.message, level: parsed.level })
      }
      if (!added.length) return
      // 既存の行と、時刻順に混ぜる(同じ時刻は、取った順)。多すぎたら、古い方を捨てる。
      setEntries((current) => [...current, ...added].sort((first, second) => first.sortKey - second.sortKey || first.id - second.id).slice(-MAX_LINES))
    }

    /** 1つの Pod の続きを取る。まだあれば直ぐに、無ければ待って(稼働中のときだけ)、もう1回。 */
    async function pull(pod: LogPodInfo) {
      // タブが裏にある間は、取らずに、待つ。
      if (document.visibilityState === "hidden") {
        if (polling && pod.running) timers.set(pod.podId, setTimeout(() => void pull(pod), IDLE_WAIT_MS))
        return
      }
      try {
        const last = since.get(pod.podId)
        const path = `/api/v1/deploy/${encodeURIComponent(deployId)}/logs?podId=${encodeURIComponent(pod.podId)}${last === undefined ? "" : `&since=${last}`}`
        const chunk = await api.get<DeployLogsResponse>(path)
        if (stopped) return
        since.set(pod.podId, chunk.nextSince)
        setError("")
        if (chunk.logs.length > 0) {
          append(pod.podId, chunk.logs)
          void pull(pod)
          return
        }
        if (polling && pod.running) timers.set(pod.podId, setTimeout(() => void pull(pod), Math.max(IDLE_WAIT_MS, refreshInterval / 2)))
      } catch (caught) {
        if (stopped) return
        setError(caught instanceof Error ? caught.message : "ログを取得できませんでした")
        if (polling && pod.running) timers.set(pod.podId, setTimeout(() => void pull(pod), IDLE_WAIT_MS * 2))
      }
    }

    // 一時停止中でも、最初の1回は、取る(画面を、空にしないため)。停止中の続きは、取らない。
    for (const pod of pods) void pull(pod)
    // 片付け: 取得を止め、待っている次の取得を取り消す。
    return () => {
      stopped = true
      for (const timer of timers.values()) clearTimeout(timer)
    }
    // pods は、集合(podKey)と稼働の状態(runningKey)が変わったときだけ、作り直す。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deployId, podKey, runningKey, polling, refreshInterval])

  // 表示する行(レベルと検索で絞る)。
  const visible = useMemo(() => {
    const query = options.query.trim().toLowerCase()
    return entries.filter((entry) => options.levels.has(entry.level) && (!options.onlyMatches || query === "" || entry.message.toLowerCase().includes(query)))
  }, [entries, options.levels, options.onlyMatches, options.query])

  // 行数を、親に伝える。
  useEffect(() => { onCount(visible.length, entries.length) }, [visible.length, entries.length, onCount])

  // 親に、クリアとダウンロードの取っ手を渡す。
  const clear = useCallback(() => setEntries([]), [])
  const download = useCallback(() => {
    // 今、画面にある行(絞り込み後)を、1つのテキストにして、ファイルとして保存する。
    const body = visible.map((entry) => `[${entry.podId.replace("pod-", "").slice(0, 4)}] ${entry.raw}`).join("\n")
    const url = URL.createObjectURL(new Blob([body + "\n"], { type: "text/plain;charset=utf-8" }))
    const link = document.createElement("a")
    link.href = url
    link.download = `${deployId}.log`
    link.click()
    URL.revokeObjectURL(url)
  }, [visible, deployId])
  useEffect(() => { onReady({ clear, download }) }, [onReady, clear, download])

  // 末尾にいるときだけ、新しい行に追従する。
  useEffect(() => {
    const element = scrollRef.current
    if (element && atBottom) element.scrollTop = element.scrollHeight
  }, [visible, atBottom])

  /** スクロールしたとき: 末尾にいるかを、覚える(上にスクロールしたら、追従を止める)。 */
  function handleScroll() {
    const element = scrollRef.current
    if (!element) return
    setAtBottom(element.scrollHeight - element.scrollTop - element.clientHeight < BOTTOM_SLACK)
  }

  /** 「最新へ」: 末尾までスクロールして、追従を再開する。 */
  function jumpToLatest() {
    const element = scrollRef.current
    if (element) element.scrollTop = element.scrollHeight
    setAtBottom(true)
  }

  return (
    <div className="log-viewer">
      <div className="log-scroll mono" ref={scrollRef} onScroll={handleScroll} role="log" aria-label="ログ" style={{ whiteSpace: options.wrap ? "pre-wrap" : "pre" }}>
        {visible.length === 0 ? (
          <div className="log-empty">{entries.length === 0 ? "ログがまだありません。deploy が起動すると、ここに出ます" : "条件に合う行がありません"}</div>
        ) : visible.map((entry) => (
          <div key={entry.id} className={`log-line lv-${entry.level}`}>
            {pods.length > 1 && <span className="log-pod" style={{ background: colors.get(entry.podId) }} title={entry.podId}>{entry.podId.replace("pod-", "").slice(0, 4)}</span>}
            {options.showTime && entry.time && <span className="log-time">{shortTime(entry.time)}</span>}
            <span className={`log-msg l-${entry.level === "error" ? "err" : entry.level === "debug" ? "dbg" : entry.level}`}>
              {highlight(entry.message, options.query.trim()).map((part, index) => part.hit ? <mark key={index} className="log-hit">{part.text}</mark> : <span key={index}>{part.text}</span>)}
            </span>
          </div>
        ))}
      </div>
      {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", margin: 8 }}>{error}</div>}
      {!atBottom && <button className="btn sm primary log-jump" type="button" onClick={jumpToLatest}>最新へ ↓</button>}
    </div>
  )
}

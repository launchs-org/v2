"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { Clipboard, Pause, Play, RefreshCw, TerminalSquare } from "lucide-react"
import { api } from "@/lib/api"
import type { DeployLogsResponse, LogPodInfo } from "@/lib/types"

export function LogTail({ deployId, pods, refreshInterval }: { deployId: string; pods: LogPodInfo[]; refreshInterval: number }) {
  const [podId, setPodId] = useState(pods.find((pod) => pod.running)?.podId ?? pods[0]?.podId ?? "")
  const [streaming, setStreaming] = useState(true)
  const [output, setOutput] = useState("")
  const [copied, setCopied] = useState(false)
  const nextSince = useRef<number | null>(null)
  const outputRef = useRef<HTMLPreElement>(null)
  const key = podId ? `/api/v1/deploy/${encodeURIComponent(deployId)}/logs?podId=${encodeURIComponent(podId)}` : null
  const fetchLogs = useCallback(async (url: string) => {
    const since = nextSince.current
    const path = since === null ? url : `${url}&since=${since}`
    const result = await api.get<DeployLogsResponse>(path)
    nextSince.current = result.nextSince
    return result
  }, [])
  const { data, error, isValidating, mutate } = useSWR<DeployLogsResponse>(key, fetchLogs, {
    refreshInterval: streaming ? refreshInterval || 5000 : 0,
    revalidateOnFocus: true,
    refreshWhenHidden: false,
    keepPreviousData: false,
  })

  useEffect(() => {
    if (data?.logs) setOutput((current) => `${current}${data.logs}`)
  }, [data?.logs, data?.nextSince])

  useEffect(() => {
    if (outputRef.current) outputRef.current.scrollTop = outputRef.current.scrollHeight
  }, [output])

  function changePod(nextPodId: string) {
    setPodId(nextPodId)
    setOutput("")
    nextSince.current = null
  }

  async function copyLogs() {
    try {
      await navigator.clipboard.writeText(output)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="log-panel">
      <div className="log-toolbar">
        <label className="sr-only" htmlFor="log-pod-select">ログ対象 Pod</label>
        <select id="log-pod-select" className="select-control log-pod-select" value={podId} onChange={(event) => changePod(event.target.value)} disabled={!pods.length}>
          {pods.length ? pods.map((pod) => <option key={pod.podId} value={pod.podId}>{pod.podId.slice(0, 22)} · {pod.running ? "実行中" : "停止"}</option>) : <option value="">Pod がありません</option>}
        </select>
        <div className="log-toolbar-actions"><span className={`streaming-indicator ${streaming ? "on" : ""}`}><span className="live-dot" />{streaming ? "LIVE" : "PAUSED"}</span><button className="icon-button" type="button" onClick={() => setStreaming((active) => !active)} title={streaming ? "ログ取得を一時停止" : "ログ取得を再開"} aria-label={streaming ? "ログ取得を一時停止" : "ログ取得を再開"}>{streaming ? <Pause size={14} /> : <Play size={14} />}</button><button className="icon-button" type="button" onClick={() => void mutate()} title="ログを更新" aria-label="ログを更新"><RefreshCw size={14} className={isValidating ? "spin-icon" : ""} /></button><button className="icon-button" type="button" onClick={() => void copyLogs()} title="ログをコピー" aria-label="ログをコピー"><Clipboard size={14} /></button></div>
      </div>
      {error && <div className="log-error">ログを取得できませんでした。{error instanceof Error ? ` ${error.message}` : ""}</div>}
      <pre ref={outputRef} className="logs-output" aria-label="コンテナログ">
        {output || <span className="logs-empty"><TerminalSquare size={16} />{pods.length ? "ログを待機しています…" : "ログを表示できる Pod がありません。デプロイの作成後にここへ表示されます。"}</span>}
      </pre>
      <div className="log-footer"><span>{output ? `${output.split("\n").filter(Boolean).length} lines` : "0 lines"}</span><span>{copied ? "コピーしました" : isValidating ? "ログを同期中…" : "最終同期 · 自動更新"}</span></div>
    </div>
  )
}

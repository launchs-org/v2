"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import type { LogLevel } from "@/lib/log-parse"
import type { ProjectViewProps } from "@/lib/nav"
import type { DeployDetail, DeployListResponse } from "@/lib/types"
import { LogViewer, type LogViewerHandle } from "@/components/log-viewer"
import { LoadingState } from "@/components/ui-kit"

/** レベルのチップ(上から順に表示)。 */
const LEVEL_CHIPS: { key: "all" | LogLevel; label: string }[] = [
  { key: "all", label: "ALL" },
  { key: "error", label: "ERROR" },
  { key: "warn", label: "WARN" },
  { key: "info", label: "INFO" },
  { key: "debug", label: "DEBUG" },
]

/** ログの見た目の CSS(この画面だけで使う。色は、親が決めるトークンを使う)。 */
const LOG_CSS = `
.logs-screen{display:flex;flex-direction:column;gap:12px;height:calc(100vh - 64px - 44px);min-height:360px}
.logs-bar{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.logs-bar .inp{width:auto;min-height:36px}
.logs-bar .sw{display:inline-flex;align-items:center;gap:6px;color:var(--mut);font-size:13px;cursor:pointer;white-space:nowrap}
.logs-card{position:relative;display:flex;flex-direction:column;flex:1;min-height:0;background:var(--panel);border:1px solid var(--line2);border-radius:8px;overflow:hidden}
.log-viewer{position:relative;display:flex;flex-direction:column;flex:1;min-height:0}
.log-scroll{flex:1;min-height:0;overflow:auto;padding:8px 0}
.log-line{display:flex;gap:12px;align-items:baseline;padding:1px 16px 1px 12px;border-left:3px solid transparent}
.log-pod{flex:none;color:#fff;border-radius:4px;padding:0 6px;font-size:11px;line-height:18px}
.log-msg{min-width:0;overflow-wrap:anywhere}
.log-empty{padding:32px;text-align:center}
.log-jump{position:absolute;right:18px;bottom:14px;box-shadow:0 2px 8px rgba(0,0,0,.18)}
.logs-foot{display:flex;align-items:center;gap:10px;padding:8px 14px;border-top:1px solid var(--line);background:var(--bg)}
@media (prefers-reduced-motion:no-preference){.log-jump{animation:logjump .18s ease-out}@keyframes logjump{from{opacity:0;transform:translateY(6px)}to{opacity:1;transform:none}}}
`

/**
 * プロジェクトのログの画面。deploy を選び、Pod(すべて/個別)のログを、時刻順に混ぜて見せる。
 * 検索のハイライト、レベルの色分けとフィルタ、時刻の表示切り替え、一時停止、クリア、ダウンロード、折り返しを持つ。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。0 なら、自動では取らない)。
 */
export function LogsView({ project, refreshInterval }: ProjectViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: list } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const deploys = list?.deploys ?? []
  const [pickedDeploy, setPickedDeploy] = useState("")
  const [pickedPod, setPickedPod] = useState("all")
  const [showEnded, setShowEnded] = useState(false)
  const [query, setQuery] = useState("")
  const [onlyMatches, setOnlyMatches] = useState(false)
  const [level, setLevel] = useState<"all" | LogLevel>("all")
  const [showTime, setShowTime] = useState(true)
  const [wrap, setWrap] = useState(true)
  const [paused, setPaused] = useState(false)
  const [counts, setCounts] = useState({ shown: 0, total: 0 })
  const handle = useRef<LogViewerHandle | null>(null)

  // 選んだ deploy(未選択なら、先頭)の詳細から、ログのある Pod を読む。
  const deployId = deploys.some((deploy) => deploy.deployId === pickedDeploy) ? pickedDeploy : deploys[0]?.deployId ?? ""
  const { data: detail } = useSWR<DeployDetail>(deployId ? `/api/v1/deploy/${encodeURIComponent(deployId)}` : null, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const logPods = detail?.logPods ?? []
  const endedCount = logPods.filter((pod) => !pod.running).length
  // 終了した Pod は、既定では隠す(選んでいる Pod は、終了済みでも残す)。
  const candidates = logPods.filter((pod) => showEnded || pod.running || pod.podId === pickedPod)
  // 取得する Pod: 「すべて」なら、候補の全部。個別なら、その1つ(無ければ、すべてに戻す)。
  const selectedPods = pickedPod === "all" || !candidates.some((pod) => pod.podId === pickedPod) ? candidates : candidates.filter((pod) => pod.podId === pickedPod)
  // 設定の、レベルの集合(ALL は全部)。
  const levels = new Set<LogLevel>(level === "all" ? ["error", "warn", "info", "debug"] : [level])

  // deploy を変えたら、Pod の選択を「すべて」に戻す。
  useEffect(() => { setPickedPod("all") }, [deployId])

  // 子から、操作の取っ手と、行数を受け取る(関数は、安定させる)。
  const onReady = useCallback((next: LogViewerHandle) => { handle.current = next }, [])
  const onCount = useCallback((shown: number, total: number) => setCounts({ shown, total }), [])

  // 一覧がまだ無い間。
  if (!list) return <LoadingState />
  // deploy が無いとき。
  if (deploys.length === 0) {
    return <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 10, alignItems: "flex-start" }}><h2 className="h2">ログがまだありません</h2><div className="mut">deploy が起動すると、ここに出ます。</div></div>
  }

  return (
    <div className="logs-screen">
      <style>{LOG_CSS}</style>
      {/* 上のツールバー: deploy と Pod の選択、検索、レベル。 */}
      <div className="logs-bar">
        <select className="inp mono" style={{ maxWidth: 260 }} value={deployId} onChange={(event) => setPickedDeploy(event.target.value)} aria-label="deploy を選ぶ">
          {deploys.map((deploy) => <option key={deploy.deployId} value={deploy.deployId}>{deploy.imageRef.split("/").pop()} · {deploy.deployId.replace("deploy-", "").slice(0, 8)}</option>)}
        </select>
        <select className="inp mono" value={pickedPod} onChange={(event) => setPickedPod(event.target.value)} aria-label="Pod を選ぶ">
          <option value="all">すべての Pod ({candidates.length})</option>
          {candidates.map((pod) => <option key={pod.podId} value={pod.podId}>{pod.podId.replace("pod-", "").slice(0, 8)}{pod.running ? "" : "(終了)"}</option>)}
        </select>
        {endedCount > 0 && <label className="sw"><input type="checkbox" checked={showEnded} onChange={(event) => setShowEnded(event.target.checked)} />終了した Pod ({endedCount})</label>}
        <label className="srch" style={{ width: 240, height: 36 }}>
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="M20 20l-4-4" /></svg>
          <input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ログを検索" aria-label="ログを検索" />
        </label>
        <label className="sw"><input type="checkbox" checked={onlyMatches} onChange={(event) => setOnlyMatches(event.target.checked)} />一致する行だけ</label>
        <div className="grow" />
        <div className="row" style={{ gap: 4 }} role="group" aria-label="レベル">
          {LEVEL_CHIPS.map((chip) => <button key={chip.key} type="button" className={`chip ${level === chip.key ? "on" : ""}`} style={{ minHeight: 30 }} aria-pressed={level === chip.key} onClick={() => setLevel(chip.key)}>{chip.label}</button>)}
        </div>
      </div>

      {/* ログの領域と、下のバー(状態、表示の切り替え、操作)。 */}
      <div className="logs-card">
        <LogViewer deployId={deployId} pods={selectedPods} options={{ query, onlyMatches, levels, showTime, wrap, paused }} refreshInterval={refreshInterval} onReady={onReady} onCount={onCount} />
        <div className="logs-foot">
          <span className={`badge ${!paused && refreshInterval > 0 ? "b-ok live" : "b-dim"}`}><i />{!paused && refreshInterval > 0 ? "LIVE" : "PAUSED"}</span>
          <span className="faint mono" style={{ fontSize: 12 }}>{counts.shown === counts.total ? `${counts.total.toLocaleString()} 行` : `${counts.shown.toLocaleString()} / ${counts.total.toLocaleString()} 行`}</span>
          <div className="grow" />
          <label className="sw" style={{ display: "inline-flex", alignItems: "center", gap: 6, color: "var(--mut)", fontSize: 13, cursor: "pointer" }}><input type="checkbox" checked={showTime} onChange={(event) => setShowTime(event.target.checked)} />時刻</label>
          <label className="sw" style={{ display: "inline-flex", alignItems: "center", gap: 6, color: "var(--mut)", fontSize: 13, cursor: "pointer" }}><input type="checkbox" checked={wrap} onChange={(event) => setWrap(event.target.checked)} />折り返し</label>
          <button className="btn sm" type="button" onClick={() => setPaused((current) => !current)}>{paused ? "再開" : "一時停止"}</button>
          <button className="btn sm" type="button" onClick={() => handle.current?.clear()}>クリア</button>
          <button className="btn sm" type="button" onClick={() => handle.current?.download()} disabled={counts.shown === 0}>ダウンロード</button>
        </div>
      </div>
    </div>
  )
}

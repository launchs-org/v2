"use client"

import { useEffect, useMemo, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { eventHint, relativeTime } from "@/lib/event-hints"
import { shortDateTime } from "@/lib/format-date"
import type { ProjectViewProps } from "@/lib/nav"
import type { DeployListResponse, ProjectEvent, ProjectEventListResponse } from "@/lib/types"
import { ErrorState, LoadingState } from "@/components/ui-kit"

/** 1回に取る件数(API の上限)。 */
const PAGE_SIZE = 100

/** 種類の絞り込み。 */
const TYPE_CHIPS: { key: "all" | "Warning" | "Normal"; label: string }[] = [
  { key: "all", label: "すべて" },
  { key: "Warning", label: "警告のみ" },
  { key: "Normal", label: "通常のみ" },
]

/**
 * プロジェクトの「イベント」の画面。namespace で起きた k8s の Event(イメージの取得、配置、起動の失敗など)を、新しい順に見せる。
 * 種類(警告/通常)、deploy、キーワードで絞り込める。よくある理由には、短い説明と、次にすることを添える。
 * 最初の 1 ページは、自動で更新する。「もっと古いイベント」で、続きを読む。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。0 なら止める)。
 * @param props.nav 画面を移る操作(deploy を開く)。
 */
export function EventsView({ project, refreshInterval, nav }: ProjectViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const [type, setType] = useState<"all" | "Warning" | "Normal">("all")
  const [deployId, setDeployId] = useState("")
  const [query, setQuery] = useState("")
  // 「もっと古いイベント」で読んだ分(絞り込みを変えたら、捨てる)。
  const [older, setOlder] = useState<ProjectEvent[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState("")
  // 「N 分前」の表示を、定期的に更新するための、いまの時刻。
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 15_000)
    return () => window.clearInterval(timer)
  }, [])

  // サーバーに渡す絞り込み(種類と deploy。キーワードは、読んだ分に、画面側でかける)。
  const filter = useMemo(() => {
    const params = new URLSearchParams({ limit: String(PAGE_SIZE) })
    if (type !== "all") params.set("type", type)
    if (deployId) params.set("deployId", deployId)
    return params
  }, [type, deployId])
  const { data, error, mutate } = useSWR<ProjectEventListResponse>(`${base}/events?${filter.toString()}`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true, refreshWhenHidden: false })
  const { data: deployList } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, { refreshInterval: 0 })

  // 絞り込みを変えたら、続きの読み込みを、捨てる。
  useEffect(() => { setOlder([]); setCursor(undefined); setMoreError("") }, [type, deployId])

  // 最初の 1 ページ(自動で更新される)と、続きで読んだ分を、つなぐ。重複は、通し番号で取り除く。
  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor
  const merged = useMemo(() => {
    const seen = new Set<string>()
    return [...(data?.events ?? []), ...older].filter((event) => (seen.has(event.eventId) ? false : (seen.add(event.eventId), true)))
  }, [data, older])

  // キーワードで絞る(理由、本文、対象の名前。大文字小文字は、区別しない)。
  const needle = query.trim().toLowerCase()
  const shown = needle ? merged.filter((event) => `${event.reason} ${event.message} ${event.objectName}`.toLowerCase().includes(needle)) : merged
  const warningCount = merged.filter((event) => event.type === "Warning").length

  /** 「もっと古いイベント」: 次のページを読んで、後ろにつなぐ。 */
  async function loadMore() {
    if (!nextCursor) return
    setLoadingMore(true)
    setMoreError("")
    try {
      const params = new URLSearchParams(filter)
      params.set("cursor", nextCursor)
      const page = await api.get<ProjectEventListResponse>(`${base}/events?${params.toString()}`)
      setOlder((current) => [...current, ...page.events])
      setCursor(page.nextCursor)
    } catch (caught) {
      setMoreError(caught instanceof Error ? caught.message : "続きを読み込めませんでした")
    } finally {
      setLoadingMore(false)
    }
  }

  return (
    <div className="events-screen">
      <style>{EVENTS_CSS}</style>
      {/* 上のバー: 種類、deploy、キーワード。 */}
      <div className="events-bar">
        <div className="row" style={{ gap: 4 }} role="group" aria-label="種類">
          {TYPE_CHIPS.map((chip) => <button key={chip.key} type="button" className={`chip ${type === chip.key ? "on" : ""}`} style={{ minHeight: 34 }} aria-pressed={type === chip.key} onClick={() => setType(chip.key)}>{chip.label}</button>)}
        </div>
        <select className="inp mono" style={{ maxWidth: 280 }} value={deployId} onChange={(event) => setDeployId(event.target.value)} aria-label="deploy で絞り込む">
          <option value="">すべての deploy</option>
          {(deployList?.deploys ?? []).map((deploy) => <option key={deploy.deployId} value={deploy.deployId}>{deploy.imageRef.split("/").pop()} · {deploy.deployId.replace("deploy-", "").slice(0, 8)}</option>)}
        </select>
        <label className="srch" style={{ width: 280, height: 38 }}>
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="M20 20l-4-4" /></svg>
          <input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="理由・メッセージ・名前で検索" aria-label="イベントを検索" />
        </label>
        <div className="grow" />
        <span className="faint mono" style={{ fontSize: 12.5 }}>{needle ? `${shown.length} / ${merged.length} 件` : `${merged.length} 件`}{warningCount > 0 && type !== "Normal" ? ` · 警告 ${warningCount}` : ""}</span>
      </div>

      {error && !data && <ErrorState message="イベントを取得できませんでした。" onRetry={() => void mutate()} />}
      {!data && !error && <div className="card"><LoadingState label="イベントを読み込んでいます" /></div>}
      {data && (
        <div className="card events-card">
          {shown.length === 0 ? (
            <div className="events-empty">
              <b>{merged.length === 0 ? "イベントは、まだありません" : "条件に合うイベントがありません"}</b>
              <div className="mut">{merged.length === 0 ? "デプロイを作ると、配置や起動の記録が、ここに並びます。" : "絞り込みを、変えてみてください。"}</div>
            </div>
          ) : (
            <ul className="events-list">
              {shown.map((event) => <EventRow key={event.eventId} event={event} now={now} onOpenDeploy={event.deployId ? () => nav.openDeploy(event.deployId as string) : undefined} />)}
            </ul>
          )}
          {(nextCursor || moreError) && (
            <div className="events-more">
              {moreError && <span className="err" style={{ fontSize: 13 }}>{moreError}</span>}
              {nextCursor && <button className="btn sm" type="button" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? "読み込み中…" : "もっと古いイベント"}</button>}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * イベント 1 件の行。警告は、左の帯と、背景で目立たせる。理由に合う、短い説明と、次にすることを、添える。
 *
 * @param props.event イベント。
 * @param props.now いまの時刻(相対表示用)。
 * @param props.onOpenDeploy deploy のパネルを開く操作(deploy に属するイベントだけ)。
 */
function EventRow({ event, now, onOpenDeploy }: { event: ProjectEvent; now: number; onOpenDeploy?: () => void }) {
  const hint = eventHint(event.reason, event.message)
  const warning = event.type === "Warning"
  return (
    <li className={`event-row ${warning ? "warn" : ""}`}>
      <div className="event-head">
        <span className={`badge ${warning ? "b-warn" : "b-dim"}`}>{warning ? "警告" : "通常"}</span>
        <b className="mono">{event.reason || "(理由なし)"}</b>
        {event.count > 1 && <span className="badge b-dim" title="同じイベントが、起きた回数">×{event.count}</span>}
        <span className="grow" />
        <span className="faint" style={{ fontSize: 12.5 }} title={`最初: ${shortDateTime(event.firstSeenAt)}`}>{relativeTime(event.lastSeenAt, now)} · {shortDateTime(event.lastSeenAt)}</span>
      </div>
      {hint && <div className="event-hint">{hint.summary}{hint.action && <span className="mut"> {hint.action}</span>}</div>}
      <div className="event-msg mono">{event.message}</div>
      <div className="event-obj mono faint">{event.objectKind} / {event.objectName}{onOpenDeploy && <button className="lnk" type="button" onClick={onOpenDeploy} style={{ marginLeft: 10, fontSize: 12.5 }}>deploy を開く</button>}</div>
    </li>
  )
}

const EVENTS_CSS = `
.events-screen{display:flex;flex-direction:column;gap:10px;height:100%;min-height:360px}
.events-bar{display:flex;flex-wrap:wrap;align-items:center;gap:10px}
.events-bar .inp{width:auto;min-height:38px}
.events-card{flex:1;min-height:0;overflow:auto;display:flex;flex-direction:column}
.events-list{list-style:none;margin:0;padding:0}
.event-row{padding:12px 18px 12px 15px;border-bottom:1px solid var(--line);border-left:3px solid transparent;display:flex;flex-direction:column;gap:4px}
.event-row:hover{background:var(--panel2)}
.event-row.warn{border-left-color:#F76707;background:color-mix(in srgb,#F76707 5%,var(--panel))}
.event-head{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.event-hint{font-size:14px}
.event-msg{font-size:13px;color:var(--text);overflow-wrap:anywhere}
.event-obj{font-size:12px}
.events-empty{display:flex;flex-direction:column;align-items:center;gap:6px;padding:56px 24px;text-align:center}
.events-more{display:flex;align-items:center;justify-content:center;gap:12px;padding:14px}
`

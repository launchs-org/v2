"use client"

import { Fragment, useEffect, useMemo, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { relativeTime } from "@/lib/event-hints"
import { shortDateTime } from "@/lib/format-date"
import type { Nav } from "@/lib/nav"
import type { Project, ProjectTask, ProjectTaskListResponse, TaskProgress, TaskStatus, TaskStep } from "@/lib/types"
import { ErrorState, LoadingState } from "@/components/ui-kit"

/** 1 回に取る件数(API の上限)。 */
const PAGE_SIZE = 100

/** 実行中のタスクがあるときの、自動更新の間隔(ミリ秒)。進み具合が、動いて見えるように、短くする。 */
const LIVE_INTERVAL_MS = 3000

/** 種類(kind)の表示名。 */
const KIND_LABELS: Record<string, string> = {
  project_create: "プロジェクトの作成", project_delete: "プロジェクトの削除",
  deploy_create: "deploy の作成", deploy_delete: "deploy の削除", deploy_config: "起動設定の適用", deploy_files: "ファイルの適用",
  deploy_mounts: "volume のマウント", deploy_configmap_mounts: "ConfigMap のマウント", deploy_scale: "スケール", deploy_crash_stop: "異常による停止",
  service_create: "service の作成", service_delete: "service の削除", service_ports: "service のポートの適用",
  volume_resize: "volume の拡張", volume_delete: "volume の削除",
  ingress_apply: "ingress のルールの適用", ingress_reapply: "ingress の再反映", ingress_delete: "ingress の削除",
  template_apply: "テンプレートの適用",
}

/** 状態の表示名と、バッジのクラス。 */
const STATUS_VIEW: Record<TaskStatus, { label: string; cls: string }> = {
  running: { label: "実行中", cls: "run" },
  succeeded: { label: "成功", cls: "ok" },
  failed: { label: "失敗", cls: "s5" },
  canceled: { label: "取り消し", cls: "canceled" },
  lost: { label: "不明", cls: "lost" },
}

/** 状態の絞り込み。 */
const STATUS_CHIPS: { key: "" | TaskStatus; label: string }[] = [
  { key: "", label: "すべて" },
  { key: "running", label: "実行中" },
  { key: "failed", label: "失敗" },
  { key: "succeeded", label: "成功" },
]

/**
 * かかった時間を、読みやすい文字にする。
 *
 * @param milliseconds ミリ秒。
 * @returns 「850 ms」「12.3 秒」「4 分 5 秒」の形。
 */
function formatDuration(milliseconds: number): string {
  // 単位を、長さで選ぶ。
  if (milliseconds < 1000) return `${milliseconds} ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(1)} 秒`
  return `${Math.floor(milliseconds / 60_000)} 分 ${Math.floor((milliseconds % 60_000) / 1000)} 秒`
}

/**
 * 状態のバッジ。実行中は、回転マークを付ける。
 *
 * @param props.status 状態。
 */
function TaskStatusBadge({ status }: { status: TaskStatus }) {
  // 表示名とクラスを引く。
  const view = STATUS_VIEW[status]
  return <span className={`alog-status ${view.cls}`}>{status === "running" && <span className="spinner" aria-hidden />}{view.label}</span>
}

/**
 * 進行バー。手順の総数が分かれば、終わった割合を、分からなければ、動き続ける帯を出す。
 *
 * @param props.progress 進み具合。
 * @param props.status タスクの状態(バーの色を決める)。
 */
function ProgressBar({ progress, status }: { progress: TaskProgress; status: TaskStatus }) {
  // 総数が無ければ、割合が出せないので、動き続ける帯にする。
  const known = progress.total > 0
  const percent = known ? Math.min(100, Math.round((progress.done / progress.total) * 100)) : 0
  const cls = status === "failed" ? "fail" : status === "succeeded" ? "done" : known ? "" : "ind"
  return <div className={`tprog ${cls}`} role="progressbar" aria-valuemin={0} aria-valuemax={known ? progress.total : undefined} aria-valuenow={known ? progress.done : undefined}><i style={known ? { width: `${status === "succeeded" ? 100 : Math.max(percent, status === "running" ? 6 : 0)}%` } : undefined} /></div>
}

/**
 * 手順(Activity)の一覧。終わった手順は緑のチェック、動いている手順は回転マーク、失敗は赤、これからの手順は白抜きの丸。
 *
 * @param props.steps 手順。
 */
function StepList({ steps }: { steps: TaskStep[] }) {
  // 1 行ずつ、状態に合う印と、やり直しの回数、失敗の理由を出す。
  return (
    <ul className="tsteps">
      {steps.map((step) => (
        <li key={step.activity} className={`${step.state === "waiting" ? "waiting" : ""} ${step.extra ? "extra" : ""}`}>
          <span className={`mark ${step.state}`} aria-hidden>{step.state === "done" ? "✓" : step.state === "failed" ? "✕" : step.state === "running" ? <span className="spinner lg" /> : ""}</span>
          <span>{step.label}{step.extra ? "(後始末)" : ""}</span>
          {step.state === "running" && (step.attempt ?? 1) > 1 && <span className="attempt">{step.attempt} 回目の試行</span>}
          {step.error && <span className="why">{step.error}</span>}
        </li>
      ))}
    </ul>
  )
}

type TasksViewProps = {
  /** 1 つのプロジェクトのタスクを見せるとき。無ければ、全てのプロジェクトのタスク(プロジェクト一覧の画面)。 */
  project?: Project
  /** 全てのプロジェクトのタスクのとき、プロジェクト名から、プロジェクトを開くための一覧。 */
  projects?: Project[]
  /** 自動更新の間隔(ミリ秒。0 なら止める)。 */
  refreshInterval: number
  nav: Nav
}

/**
 * 「タスク」の画面。deploy の作成や ingress の反映など、裏で動いた処理(Workflow)の履歴を、新しい順の表で見せる。
 * 実行中のタスクは、回転マークと進行バー(いま動いている手順)を出し、短い間隔で更新する。行を開くと、手順の一覧、理由、子の処理が見える。
 * 状態(実行中・失敗・成功)と種類で絞り込める。プロジェクト一覧では、全てのプロジェクトのタスク(削除したものを含む)を、プロジェクト名つきで出す。
 *
 * @param props.project プロジェクト(無ければ、全てのプロジェクト)。
 * @param props.projects プロジェクトの一覧(全てのプロジェクトのとき、名前から開く)。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。0 なら止める)。
 * @param props.nav 画面を移る操作(対象の deploy、ingress、volume、プロジェクトを開く)。
 */
export function TasksView({ project, projects, refreshInterval, nav }: TasksViewProps) {
  const base = project ? `/api/v1/projects/${encodeURIComponent(project.projectId)}/tasks` : "/api/v1/tasks"
  const [status, setStatus] = useState<"" | TaskStatus>("")
  const [kind, setKind] = useState("")
  const [older, setOlder] = useState<ProjectTask[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState("")
  // 開いている行(詳細)と、その詳細(手順と、子のタスクを含む)。
  const [openId, setOpenId] = useState<string | null>(null)
  const [detail, setDetail] = useState<ProjectTask | null>(null)
  // 「N 分前」の表示を、定期的に更新するための、いまの時刻。
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 15_000)
    return () => window.clearInterval(timer)
  }, [])

  // サーバーに渡す絞り込み。
  const filter = useMemo(() => {
    const params = new URLSearchParams({ limit: String(PAGE_SIZE) })
    if (status) params.set("status", status)
    if (kind) params.set("kind", kind)
    return params
  }, [status, kind])
  // 実行中のタスクがあれば、短い間隔で更新する(進み具合が、動いて見える)。
  const { data, error, isValidating, mutate } = useSWR<ProjectTaskListResponse>(`${base}?${filter.toString()}`, authenticatedFetcher, {
    refreshInterval: (latest) => (refreshInterval > 0 && (latest?.counts.running ?? 0) > 0 ? Math.min(refreshInterval, LIVE_INTERVAL_MS) : refreshInterval),
    revalidateOnFocus: true, refreshWhenHidden: false,
  })

  // 絞り込みを変えたら、続きと、開いた行を捨てる。
  useEffect(() => { setOlder([]); setCursor(undefined); setMoreError(""); setOpenId(null); setDetail(null) }, [status, kind])

  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor
  const rows = useMemo(() => {
    const seen = new Set<string>()
    return [...(data?.tasks ?? []), ...older].filter((task) => (seen.has(task.taskId) ? false : (seen.add(task.taskId), true)))
  }, [data, older])

  // 開いている行は、実行中の間、詳細(手順)を、短い間隔で取り直す。
  const openTask = rows.find((task) => task.taskId === openId)
  const openRunning = openTask?.status === "running"
  useEffect(() => {
    if (!openId || !openRunning) return
    const timer = window.setInterval(() => { api.get<ProjectTask>(`${taskPath(openTask)}`).then(setDetail).catch(() => undefined) }, LIVE_INTERVAL_MS)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openId, openRunning])

  /**
   * タスクの詳細を取る API のパス。全てのプロジェクトの一覧でも、タスクのプロジェクトで引く。
   *
   * @param task タスク。
   * @returns パス。
   */
  function taskPath(task: ProjectTask | undefined): string {
    // プロジェクトの ID は、タスクが持っている。
    return `/api/v1/projects/${encodeURIComponent(task?.projectId ?? project?.projectId ?? "")}/tasks/${encodeURIComponent(task?.taskId ?? "")}`
  }

  /** 「さらに読み込む」: 次のページを読んで、後ろにつなぐ。 */
  async function loadMore() {
    if (!nextCursor) return
    setLoadingMore(true)
    setMoreError("")
    try {
      const params = new URLSearchParams(filter)
      params.set("cursor", nextCursor)
      const page = await api.get<ProjectTaskListResponse>(`${base}?${params.toString()}`)
      setOlder((current) => [...current, ...page.tasks])
      setCursor(page.nextCursor)
    } catch (caught) {
      setMoreError(caught instanceof Error ? caught.message : "続きを読み込めませんでした")
    } finally {
      setLoadingMore(false)
    }
  }

  /**
   * 行を開閉する。開くときは、詳細(手順と、子のタスク)を取る。
   *
   * @param task 行のタスク。
   */
  async function toggle(task: ProjectTask) {
    // 開いている行を押したら、閉じる。
    if (openId === task.taskId) { setOpenId(null); setDetail(null); return }
    setOpenId(task.taskId)
    setDetail(null)
    try { setDetail(await api.get<ProjectTask>(taskPath(task))) } catch { setDetail(task) }
  }

  /**
   * 対象のリソースを、開く操作にする(deploy、ingress、volume だけ。プロジェクト一覧では、プロジェクトを開いてから)。
   *
   * @param task タスク。
   * @returns 開く関数。開けない種類なら undefined。
   */
  function opener(task: ProjectTask): (() => void) | undefined {
    // 全てのプロジェクトの一覧では、まず、そのプロジェクトを開く(削除済みなら、開けない)。
    const owner = project ?? projects?.find((item) => item.projectId === task.projectId)
    if (!owner) return undefined
    const open = (action: () => void) => () => { if (!project) nav.openProject(owner); action() }
    if (task.resourceId.startsWith("deploy-")) return open(() => nav.openDeploy(task.resourceId))
    if (task.resourceId.startsWith("ingress-")) return open(() => nav.select({ kind: "ingress", id: task.resourceId }))
    if (task.resourceId.startsWith("volume-")) return open(() => nav.select({ kind: "volume", id: task.resourceId }))
    return undefined
  }

  const showProject = !project
  const columns = showProject ? 8 : 7
  const kindOptions = Object.entries(KIND_LABELS)
  return (
    <div className="tasks-screen">
      <div className="tasks-bar">
        <div className="row" style={{ gap: 4 }} role="group" aria-label="状態">
          {STATUS_CHIPS.map((chip) => <button key={chip.key || "all"} type="button" className={`chip ${status === chip.key ? "on" : ""}`} style={{ minHeight: 34 }} aria-pressed={status === chip.key} onClick={() => setStatus(chip.key)}>{chip.label}</button>)}
        </div>
        <select className="inp" style={{ maxWidth: 240, minHeight: 34 }} value={kind} onChange={(event) => setKind(event.target.value)} aria-label="種類で絞り込む">
          <option value="">すべての種類</option>
          {kindOptions.map(([key, label]) => <option key={key} value={key}>{label}</option>)}
        </select>
        <div className="grow" />
        {(data?.counts.running ?? 0) > 0 && <span className="row" style={{ gap: 6, color: "#1A73E8", fontSize: 12.5 }}><span className="spinner" aria-hidden />実行中 {data?.counts.running} 件</span>}
        <span className="faint mono" style={{ fontSize: 12.5 }}>{rows.length} 件</span>
        <button className="btn sm" type="button" disabled={isValidating} onClick={() => { setOlder([]); setCursor(undefined); void mutate() }}>{isValidating ? "更新中…" : "↻ 更新"}</button>
      </div>

      {error && !data && <ErrorState message="タスクを取得できませんでした。" onRetry={() => void mutate()} />}
      {!data && !error && <div className="card"><LoadingState label="タスクを読み込んでいます" /></div>}
      {data && (
        <div className="card" style={{ overflow: "hidden" }}>
          {rows.length === 0 ? (
            <div className="alog-empty">{status || kind ? "条件に合うタスクがありません。" : "タスクは、まだありません。プロジェクトや deploy、ingress を操作すると、処理の履歴が、ここに並びます。"}</div>
          ) : (
            <div className="alog-wrap" style={{ maxHeight: "none", borderTop: 0 }}>
              <table className="alog-table tasks-table">
                <thead>
                  <tr><th>開始</th>{showProject && <th>プロジェクト</th>}<th>種類</th><th>対象</th><th>状態</th><th>進み具合</th><th className="num">時間</th><th>理由</th></tr>
                </thead>
                <tbody>
                  {rows.map((task) => {
                    const open = opener(task)
                    const isOpen = openId === task.taskId
                    const owner = projects?.find((item) => item.projectId === task.projectId)
                    return (
                      <Fragment key={task.taskId}>
                        <tr className="clickable" onClick={() => void toggle(task)} aria-expanded={isOpen}>
                          <td title={shortDateTime(task.startedAt)}>{relativeTime(task.startedAt, now)}</td>
                          {showProject && <td>{owner ? <button type="button" className="lnk" onClick={(event) => { event.stopPropagation(); nav.openProject(owner) }}>{task.projectName || task.projectId.slice(0, 16)}</button> : <span className="mut" title={owner ? undefined : "削除済みのプロジェクト"}>{task.projectName || task.projectId.slice(0, 16)}{!owner && <span className="faint">(削除済み)</span>}</span>}</td>}
                          <td>{KIND_LABELS[task.kind] ?? task.kind}</td>
                          <td className="mono">{task.kind.startsWith("project_") ? "—" : open ? <button type="button" className="btn sm" style={{ minHeight: 26, padding: "0 8px" }} onClick={(event) => { event.stopPropagation(); open() }}>{task.resourceId.replace(/^[a-z]+-/, "").slice(0, 8)} ↗</button> : task.resourceId.replace(/^[a-z]+-/, "").slice(0, 8)}</td>
                          <td><TaskStatusBadge status={task.status} /></td>
                          <td>
                            {task.progress && (task.status === "running" || task.status === "failed") ? (
                              <div className="tprog-cell"><ProgressBar progress={task.progress} status={task.status} /><small>{task.progress.total > 0 ? `${task.progress.done} / ${task.progress.total}` : ""}{task.progress.current ? `${task.progress.total > 0 ? " · " : ""}${task.progress.current}` : ""}</small></div>
                            ) : task.status === "running" ? <div className="tprog-cell"><div className="tprog ind"><i /></div></div> : <span className="faint">—</span>}
                          </td>
                          <td className="num">{formatDuration(task.durationMs)}</td>
                          <td style={{ maxWidth: 320, overflow: "hidden", textOverflow: "ellipsis", color: task.error ? "var(--bad)" : "var(--faint)" }} title={task.error}>{task.error || "—"}</td>
                        </tr>
                        {isOpen && (
                          <tr className="tasks-detail">
                            <td colSpan={columns}>
                              {task.error && <div className="reason">{task.error}</div>}
                              {task.status === "failed" && <div className="mut" style={{ fontSize: 12.5, marginTop: 6 }}>原因の手がかりは、「イベント」タブ(イメージの取得、配置、起動の失敗)や、「ログ」タブにあります。</div>}
                              {!task.error && task.status === "lost" && <div className="mut" style={{ fontSize: 13 }}>終わりの記録が付いていません(worker が止まった、または、時間切れの可能性があります)。</div>}
                              {detail && detail.taskId === task.taskId && (detail.progress?.steps?.length ?? 0) > 0 && <StepList steps={detail.progress?.steps ?? []} />}
                              <div className="meta mono">開始 {shortDateTime(task.startedAt)}{task.finishedAt ? ` · 終了 ${shortDateTime(task.finishedAt)}` : ""} · Workflow {task.workflowId}</div>
                              {detail && detail.taskId === task.taskId && (detail.children?.length ?? 0) > 0 && (
                                <ul>
                                  {detail.children?.map((child) => <li key={child.taskId}><TaskStatusBadge status={child.status} /><span>{KIND_LABELS[child.kind] ?? child.kind}</span>{child.progress && child.status === "running" && <div className="tprog-cell"><ProgressBar progress={child.progress} status={child.status} /></div>}<span className="faint mono">{formatDuration(child.durationMs)}</span>{child.error && <span style={{ color: "var(--bad)" }}>{child.error}</span>}</li>)}
                                </ul>
                              )}
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
          {(nextCursor || moreError) && (
            <div className="alog-foot">
              {moreError && <span className="err">{moreError}</span>}
              {nextCursor && <button className="btn sm" type="button" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? "読み込み中…" : "さらに読み込む"}</button>}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

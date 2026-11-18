"use client"

import { Fragment, useEffect, useMemo, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { relativeTime } from "@/lib/event-hints"
import { shortDateTime } from "@/lib/format-date"
import type { ProjectViewProps } from "@/lib/nav"
import type { ProjectTask, ProjectTaskListResponse, TaskStatus } from "@/lib/types"
import { ErrorState, LoadingState } from "@/components/ui-kit"

/** 1 回に取る件数(API の上限)。 */
const PAGE_SIZE = 100

/** 種類(kind)の表示名。 */
const KIND_LABELS: Record<string, string> = {
  deploy_create: "deploy の作成", deploy_delete: "deploy の削除", deploy_config: "起動設定の適用", deploy_files: "ファイルの適用",
  deploy_mounts: "volume のマウント", deploy_configmap_mounts: "ConfigMap のマウント", deploy_scale: "スケール", deploy_crash_stop: "異常による停止",
  service_create: "service の作成", service_delete: "service の削除", service_ports: "service のポートの適用",
  volume_resize: "volume の拡張", volume_delete: "volume の削除",
  ingress_apply: "ingress のルールの適用", ingress_reapply: "ingress の再反映", ingress_delete: "ingress の削除",
  template_apply: "テンプレートの適用", project_delete: "プロジェクトの削除",
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
 * 状態のバッジ。
 *
 * @param props.status 状態。
 */
function TaskStatusBadge({ status }: { status: TaskStatus }) {
  // 表示名とクラスを引く。
  const view = STATUS_VIEW[status]
  return <span className={`alog-status ${view.cls}`}>{view.label}</span>
}

/**
 * プロジェクトの「タスク」の画面。deploy の作成や ingress の反映など、裏で動いた処理(Workflow)の履歴を、新しい順の表で見せる。
 * 状態(実行中・失敗・成功)と種類で絞り込める。失敗した行を開くと、理由と、子の処理が見える。
 * 先頭のページは、自動で更新する。「さらに読み込む」で、古い分を読む。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。0 なら止める)。
 * @param props.nav 画面を移る操作(対象の deploy、ingress、volume を開く)。
 */
export function TasksView({ project, refreshInterval, nav }: ProjectViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}/tasks`
  const [status, setStatus] = useState<"" | TaskStatus>("")
  const [kind, setKind] = useState("")
  const [older, setOlder] = useState<ProjectTask[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState("")
  // 開いている行(詳細)と、その詳細(子のタスクを含む)。
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
  const { data, error, isValidating, mutate } = useSWR<ProjectTaskListResponse>(`${base}?${filter.toString()}`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true, refreshWhenHidden: false })

  // 絞り込みを変えたら、続きと、開いた行を捨てる。
  useEffect(() => { setOlder([]); setCursor(undefined); setMoreError(""); setOpenId(null); setDetail(null) }, [status, kind])

  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor
  const rows = useMemo(() => {
    const seen = new Set<string>()
    return [...(data?.tasks ?? []), ...older].filter((task) => (seen.has(task.taskId) ? false : (seen.add(task.taskId), true)))
  }, [data, older])

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
   * 行を開閉する。開くときは、詳細(子のタスク)を取る。
   *
   * @param task 行のタスク。
   */
  async function toggle(task: ProjectTask) {
    // 開いている行を押したら、閉じる。
    if (openId === task.taskId) { setOpenId(null); setDetail(null); return }
    setOpenId(task.taskId)
    setDetail(null)
    try { setDetail(await api.get<ProjectTask>(`${base}/${encodeURIComponent(task.taskId)}`)) } catch { setDetail(task) }
  }

  /**
   * 対象のリソースを、開く操作にする(deploy、ingress、volume だけ)。
   *
   * @param task タスク。
   * @returns 開く関数。開けない種類なら undefined。
   */
  function opener(task: ProjectTask): (() => void) | undefined {
    // ID の接頭辞で、種類を決める。
    if (task.resourceId.startsWith("deploy-")) return () => nav.openDeploy(task.resourceId)
    if (task.resourceId.startsWith("ingress-")) return () => nav.select({ kind: "ingress", id: task.resourceId })
    if (task.resourceId.startsWith("volume-")) return () => nav.select({ kind: "volume", id: task.resourceId })
    return undefined
  }

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
        <span className="faint mono" style={{ fontSize: 12.5 }}>{rows.length} 件</span>
        <button className="btn sm" type="button" disabled={isValidating} onClick={() => { setOlder([]); setCursor(undefined); void mutate() }}>{isValidating ? "更新中…" : "↻ 更新"}</button>
      </div>

      {error && !data && <ErrorState message="タスクを取得できませんでした。" onRetry={() => void mutate()} />}
      {!data && !error && <div className="card"><LoadingState label="タスクを読み込んでいます" /></div>}
      {data && (
        <div className="card" style={{ overflow: "hidden" }}>
          {rows.length === 0 ? (
            <div className="alog-empty">{status || kind ? "条件に合うタスクがありません。" : "タスクは、まだありません。deploy や ingress を操作すると、裏の処理が、ここに並びます。"}</div>
          ) : (
            <div className="alog-wrap" style={{ maxHeight: "none", borderTop: 0 }}>
              <table className="alog-table tasks-table">
                <thead>
                  <tr><th>開始</th><th>種類</th><th>対象</th><th>状態</th><th className="num">時間</th><th>理由</th></tr>
                </thead>
                <tbody>
                  {rows.map((task) => {
                    const open = opener(task)
                    const isOpen = openId === task.taskId
                    return (
                      <Fragment key={task.taskId}>
                        <tr className="clickable" onClick={() => void toggle(task)} aria-expanded={isOpen}>
                          <td title={shortDateTime(task.startedAt)}>{relativeTime(task.startedAt, now)}</td>
                          <td>{KIND_LABELS[task.kind] ?? task.kind}</td>
                          <td className="mono">{open ? <button type="button" className="btn sm" style={{ minHeight: 26, padding: "0 8px" }} onClick={(event) => { event.stopPropagation(); open() }}>{task.resourceId.replace(/^[a-z]+-/, "").slice(0, 8)} ↗</button> : task.resourceId.replace(/^[a-z]+-/, "").slice(0, 8)}</td>
                          <td><TaskStatusBadge status={task.status} /></td>
                          <td className="num">{formatDuration(task.durationMs)}</td>
                          <td style={{ maxWidth: 360, overflow: "hidden", textOverflow: "ellipsis", color: task.error ? "var(--bad)" : "var(--faint)" }} title={task.error}>{task.error || "—"}</td>
                        </tr>
                        {isOpen && (
                          <tr className="tasks-detail">
                            <td colSpan={6}>
                              {task.error && <div className="reason">{task.error}</div>}
                              {task.status === "failed" && <div className="mut" style={{ fontSize: 12.5, marginTop: 6 }}>原因の手がかりは、「イベント」タブ(イメージの取得、配置、起動の失敗)や、「ログ」タブにあります。</div>}
                              {!task.error && task.status === "lost" && <div className="mut" style={{ fontSize: 13 }}>終わりの記録が付いていません(worker が止まった、または、時間切れの可能性があります)。</div>}
                              {!task.error && task.status !== "lost" && <div className="mut" style={{ fontSize: 13 }}>{task.status === "running" ? "実行中です。" : "問題なく終わりました。"}</div>}
                              <div className="meta mono">開始 {shortDateTime(task.startedAt)}{task.finishedAt ? ` · 終了 ${shortDateTime(task.finishedAt)}` : ""} · Workflow {task.workflowId}</div>
                              {detail && detail.taskId === task.taskId && (detail.children?.length ?? 0) > 0 && (
                                <ul>
                                  {detail.children?.map((child) => <li key={child.taskId}><TaskStatusBadge status={child.status} /><span>{KIND_LABELS[child.kind] ?? child.kind}</span><span className="faint mono">{formatDuration(child.durationMs)}</span>{child.error && <span style={{ color: "var(--bad)" }}>{child.error}</span>}</li>)}
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

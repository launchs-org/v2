"use client"

import { useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import { validateProjectName } from "@/lib/project-name"
import type { DeployListResponse, Project, VolumeListResponse } from "@/lib/types"
import { Badge, EmptyState, Modal, ToastNotice } from "@/components/ui-kit"
import { TasksView } from "@/components/views/tasks-view"

type ProjectsViewProps = Pick<ProjectViewProps, "nav" | "refreshInterval"> & { search: string; createTick: number; projects: Project[]; onRefresh: () => Promise<unknown> }

/**
 * プロジェクトのカード 1 枚(デプロイ数とボリューム数は、カードごとに取る)。カード全体を押すと、そのプロジェクトを開く。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.nav 画面を移る操作。
 * @param props.onRename 名前変更のボタンが押されたとき。
 * @param props.onDelete 削除のボタンが押されたとき。
 */
function ProjectCard({ project, refreshInterval, nav, onRename, onDelete }: { project: Project; refreshInterval: number; nav: ProjectsViewProps["nav"]; onRename: () => void; onDelete: () => void }) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: deploys } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, { refreshInterval })
  const { data: volumes } = useSWR<VolumeListResponse>(`${base}/volumes?limit=100`, authenticatedFetcher, { refreshInterval })
  // 削除中・削除失敗の間は、開けない・変えられない(削除失敗は、削除をやり直せる)。
  const locked = project.status === "deleting"
  return (
    <div className={`pcard ${locked ? "locked" : ""}`}>
      {/* カード全体を、開くボタンにする(下の操作ボタンは、別に押せる)。 */}
      <button className="pcard-open" type="button" onClick={() => nav.openProject(project)} disabled={locked} aria-label={`${project.name} を開く`}>
        <div className="row" style={{ gap: 12, flexWrap: "nowrap", alignItems: "flex-start" }}>
          <span className="pavatar" aria-hidden>{project.name.slice(0, 1).toUpperCase()}</span>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div className="pname disp" title={project.name}>{project.name}</div>
            <div className="faint mono pid">{project.projectId.slice(0, 22)}</div>
          </div>
          <Badge status={project.status} />
        </div>
        <div className="pstats">
          <div><b className="mono">{deploys ? deploys.deploys.length : "…"}</b><span className="faint">デプロイ</span></div>
          <div><b className="mono">{volumes ? volumes.volumes.length : "…"}</b><span className="faint">ボリューム</span></div>
        </div>
      </button>
      <div className="pcard-foot">
        <button className="btn sm ghost" type="button" onClick={onRename} disabled={project.status !== "active"}>名前変更</button>
        <button className="btn sm ghost danger" type="button" onClick={onDelete} disabled={locked}>削除</button>
      </div>
    </div>
  )
}

/**
 * プロジェクトの一覧の画面(作成、名前の変更、削除)。
 *
 * @param props.projects プロジェクトの一覧。
 * @param props.onRefresh 一覧を取り直す。
 * @param props.search 検索の文字。
 * @param props.createTick 増えたら、作成のダイアログを開く。
 */
export function ProjectsView({ projects, refreshInterval, onRefresh, nav, search, createTick }: ProjectsViewProps) {
  const [modal, setModal] = useState<"create" | "rename" | "delete" | null>(null)
  const [target, setTarget] = useState<Project | null>(null)
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)
  const lastTick = useRef(createTick)
  const keyword = search.trim().toLowerCase()
  const rows = projects.filter((project) => !keyword || project.name.toLowerCase().includes(keyword) || project.projectId.toLowerCase().includes(keyword))
  const problem = validateProjectName(name.trim())

  // 上のバーの「作成」が押されたら、作成のダイアログを開く。
  useEffect(() => {
    if (createTick !== lastTick.current) {
      lastTick.current = createTick
      setName("")
      setError("")
      setModal("create")
    }
  }, [createTick])

  /** 作成か名前変更を送る。作成したら、そのプロジェクトを開く。 */
  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (problem) return
    setBusy(true)
    setError("")
    try {
      if (modal === "create") {
        const created = await api.post<{ projectId: string }>("/api/v1/projects", { name: name.trim() })
        await onRefresh()
        setModal(null)
        nav.openProject({ projectId: created.projectId, name: name.trim(), status: "active" })
      } else if (modal === "rename" && target) {
        await api.patch(`/api/v1/projects/${encodeURIComponent(target.projectId)}`, { name: name.trim() })
        await onRefresh()
        setModal(null)
        setToast({ message: "名前を変更しました", error: false })
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "保存できませんでした")
    } finally {
      setBusy(false)
    }
  }

  /** 削除を頼む(実体の削除は、非同期に進む)。 */
  async function remove() {
    if (!target) return
    setBusy(true)
    try {
      await api.delete(`/api/v1/projects/${encodeURIComponent(target.projectId)}`)
      await onRefresh()
      setModal(null)
      setToast({ message: "削除を受け付けました", error: false })
    } catch (caught) {
      setModal(null)
      setToast({ message: caught instanceof Error ? caught.message : "削除できませんでした", error: true })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div><h1 className="h1 disp">プロジェクト</h1></div>
      {projects.length === 0 && <EmptyState title="プロジェクトがありません" description="右上のボタンから、最初のプロジェクトを作成してください。" />}
      {projects.length > 0 && (
        <div className="pgrid">
          {rows.map((project) => <ProjectCard key={project.projectId} project={project} refreshInterval={refreshInterval} nav={nav} onRename={() => { setTarget(project); setName(project.name); setError(""); setModal("rename") }} onDelete={() => { setTarget(project); setModal("delete") }} />)}
          {/* 新しく作るカード(上のバーの「作成」と同じ)。 */}
          <button className="pcard pnew" type="button" onClick={() => { setName(""); setError(""); setModal("create") }}><span className="pplus" aria-hidden>+</span><span>新しいプロジェクト</span></button>
          {rows.length === 0 && <div className="mut" style={{ gridColumn: "1 / -1" }}>一致するプロジェクトがありません。</div>}
        </div>
      )}
      {/* 全てのプロジェクトのタスク(作成・削除を含む。削除したプロジェクトのものも)。 */}
      <div style={{ marginTop: 28 }}>
        <h2 className="h2 disp" style={{ marginBottom: 12 }}>タスク</h2>
        <TasksView projects={projects} refreshInterval={refreshInterval} nav={nav} />
      </div>
      {(modal === "create" || modal === "rename") && (
        <Modal title={modal === "create" ? "プロジェクトを作成" : "名前を変更"} onClose={() => setModal(null)} footer={<><button className="btn" type="button" onClick={() => setModal(null)}>やめる</button><button className="btn primary" type="submit" form="project-form" disabled={busy || problem !== null}>{modal === "create" ? "作成" : "保存"}</button></>}>
          <form id="project-form" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <label className="lbl">プロジェクト名<input className="inp" value={name} onChange={(event) => setName(event.target.value)} maxLength={64} placeholder="my-app" autoFocus /></label>
            <div className="faint" style={{ fontSize: 12.5 }}>英数字とハイフン、1〜64文字。</div>
            {name.trim() !== "" && problem && <div className="err" role="alert">{problem}</div>}
            {error && <div className="err" role="alert">{error}</div>}
          </form>
        </Modal>
      )}
      {modal === "delete" && target && (
        <Modal title="プロジェクトを削除しますか?" onClose={() => setModal(null)} footer={<><button className="btn" type="button" onClick={() => setModal(null)}>やめる</button><button className="btn warn" type="button" onClick={() => void remove()} disabled={busy}>削除する</button></>}>
          <div><b>{target.name}</b> と、配下のデプロイ・Ingress・ボリュームを削除します。元に戻せません。</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => setToast(null)} />}
    </>
  )
}

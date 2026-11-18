"use client"

import { useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import { validateProjectName } from "@/lib/project-name"
import type { DeployListResponse, Project, VolumeListResponse } from "@/lib/types"
import { Badge, EmptyState, Modal, ToastNotice } from "@/components/ui-kit"

/** 表の列幅(モックと同じ)。 */
const COLS = { "--cols": "1.4fr 1fr 90px 100px 130px 190px" } as React.CSSProperties

type ProjectsViewProps = Pick<ProjectViewProps, "nav" | "refreshInterval"> & { search: string; createTick: number; projects: Project[]; onRefresh: () => Promise<unknown> }

/**
 * プロジェクト1行(デプロイ数とボリューム数は、行ごとに取る)。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.nav 画面を移る操作。
 * @param props.onRename 名前変更のボタンが押されたとき。
 * @param props.onDelete 削除のボタンが押されたとき。
 */
function ProjectRow({ project, refreshInterval, nav, onRename, onDelete }: { project: Project; refreshInterval: number; nav: ProjectsViewProps["nav"]; onRename: () => void; onDelete: () => void }) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: deploys } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, { refreshInterval })
  const { data: volumes } = useSWR<VolumeListResponse>(`${base}/volumes?limit=100`, authenticatedFetcher, { refreshInterval })
  // 削除中・削除失敗の間は、開けない・変えられない(削除失敗は、削除をやり直せる)。
  const locked = project.status === "deleting"
  return (
    <div className="tr" style={COLS}>
      <button className="lnk" type="button" style={{ fontSize: 14 }} onClick={() => nav.openProject(project)} disabled={locked}>{project.name}</button>
      <span className="faint mono" style={{ wordBreak: "break-all", fontSize: 11.5 }}>{project.projectId.slice(0, 22)}</span>
      <span className="mono">{deploys ? deploys.deploys.length : "…"}</span>
      <span className="mono">{volumes ? volumes.volumes.length : "…"}</span>
      <span><Badge status={project.status} /></span>
      <div className="row" style={{ gap: 6, flexWrap: "nowrap", justifyContent: "flex-end" }}>
        <button className="btn sm" type="button" onClick={onRename} disabled={project.status !== "active"}>名前変更</button>
        <button className="btn sm danger" type="button" onClick={onDelete} disabled={locked}>削除</button>
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
        <div className="card tscroll"><div className="tinner">
          <div className="tr th" style={COLS}><span>名前</span><span>ID</span><span>デプロイ</span><span>ボリューム</span><span>状態</span><span /></div>
          {rows.map((project) => <ProjectRow key={project.projectId} project={project} refreshInterval={refreshInterval} nav={nav} onRename={() => { setTarget(project); setName(project.name); setError(""); setModal("rename") }} onDelete={() => { setTarget(project); setModal("delete") }} />)}
          {rows.length === 0 && <div className="tr mut" style={{ "--cols": "1fr" } as React.CSSProperties}>一致するプロジェクトがありません。</div>}
        </div></div>
      )}
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

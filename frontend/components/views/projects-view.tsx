"use client"

import { useState } from "react"
import { FolderKanban, Pencil, Plus, Trash2 } from "lucide-react"
import { api } from "@/lib/api"
import type { Project, ProjectListResponse } from "@/lib/types"
import { EmptyState, ErrorState, Modal, PageHeading, StatusPill, ToastNotice } from "@/components/ui-kit"

type ProjectsViewProps = {
  projects: Project[]
  loading: boolean
  onRefresh: () => Promise<ProjectListResponse | undefined>
  onSelect: (project: Project) => void
}

export function ProjectsView({ projects, loading, onRefresh, onSelect }: ProjectsViewProps) {
  const [modal, setModal] = useState<"create" | "rename" | "delete" | null>(null)
  const [target, setTarget] = useState<Project | null>(null)
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState("")
  const [error, setError] = useState("")

  function openCreate() {
    setTarget(null)
    setName("")
    setError("")
    setModal("create")
  }

  function openRename(project: Project) {
    setTarget(project)
    setName(project.name)
    setError("")
    setModal("rename")
  }

  async function submitProject(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      const normalizedName = name.trim()
      if (modal === "create") {
        const result = await api.post<{ projectId: string }>("/api/v1/projects", { name: normalizedName })
        const updated = await onRefresh()
        const created = updated?.projects.find((project) => project.projectId === result.projectId)
        setModal(null)
        setToast("プロジェクトを作成しました")
        if (created) onSelect(created)
      } else if (modal === "rename" && target) {
        await api.patch(`/api/v1/projects/${encodeURIComponent(target.projectId)}`, { name: normalizedName })
        await onRefresh()
        setModal(null)
        setToast("プロジェクト名を更新しました")
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "プロジェクトを保存できませんでした")
    } finally {
      setBusy(false)
    }
  }

  async function deleteProject() {
    if (!target) return
    setBusy(true)
    setError("")
    try {
      await api.delete(`/api/v1/projects/${encodeURIComponent(target.projectId)}`)
      await onRefresh()
      setModal(null)
      setToast("削除を受け付けました。配下リソースを非同期で削除しています")
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "プロジェクトを削除できませんでした")
    } finally {
      setBusy(false)
    }
  }

  return (
    <section>
      <PageHeading eyebrow="WORKSPACE" title="プロジェクト" description="環境ごとのコンテナ、デプロイ、ストレージをまとめて管理します。" actions={<button className="btn btn-primary" type="button" onClick={openCreate}><Plus size={15} />新規プロジェクト</button>} />
      {error && !modal && <ErrorState message={error} />}
      {loading ? <div className="skeleton-table"><div /><div /><div /></div> : projects.length === 0 ? <div className="card"><EmptyState icon={<FolderKanban size={19} />} title="プロジェクトがありません" description="最初のプロジェクトを作成して、デプロイやリソースの管理を始めましょう。" action={<button className="btn btn-primary" type="button" onClick={openCreate}><Plus size={15} />プロジェクトを作成</button>} /></div> : (
        <div className="card table-card">
          <div className="table-toolbar"><div><strong>すべてのプロジェクト</strong><span className="muted-inline">{projects.length}件</span></div><span className="small-muted">リストは自動更新されます</span></div>
          <div className="table-scroll"><table className="data-table"><thead><tr><th>プロジェクト名</th><th>状態</th><th>識別子</th><th className="align-right">操作</th></tr></thead><tbody>
            {projects.map((project) => <tr key={project.projectId}>
              <td><button className="project-cell-button" type="button" onClick={() => onSelect(project)}><span className="table-icon"><FolderKanban size={15} /></span><span className="project-cell-name">{project.name}</span></button></td>
              <td><StatusPill status={project.status} /></td>
              <td><code className="id-cell">{project.projectId}</code></td>
              <td><div className="row-actions"><button className="icon-button" type="button" onClick={() => openRename(project)} aria-label={`${project.name} の名前を変更`} title="名前を変更" disabled={project.status !== "active"}><Pencil size={14} /></button><button className="icon-button danger-icon" type="button" onClick={() => { setTarget(project); setError(""); setModal("delete") }} aria-label={`${project.name} を削除`} title="削除" disabled={project.status === "deleting"}><Trash2 size={14} /></button></div></td>
            </tr>)}
          </tbody></table></div>
        </div>
      )}
      {modal === "create" && <Modal title="プロジェクトを作成" description="英数字とハイフンを使用した1〜64文字の名前を指定してください。" onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-primary" form="project-form" type="submit" disabled={busy || !name.trim()}>{busy ? "作成中…" : "作成する"}</button></>}><form id="project-form" onSubmit={submitProject}><label className="form-label" htmlFor="project-name">プロジェクト名</label><input id="project-name" className="form-control" value={name} onChange={(event) => setName(event.target.value)} pattern="[a-zA-Z0-9-]{1,64}" maxLength={64} placeholder="production-api" required autoFocus />{error && <div className="form-error">{error}</div>}</form></Modal>}
      {modal === "rename" && target && <Modal title="プロジェクト名を変更" description={`${target.name} の表示名を更新します。`} onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-primary" form="project-form" type="submit" disabled={busy || !name.trim()}>{busy ? "保存中…" : "変更を保存"}</button></>}><form id="project-form" onSubmit={submitProject}><label className="form-label" htmlFor="project-name">新しい名前</label><input id="project-name" className="form-control" value={name} onChange={(event) => setName(event.target.value)} pattern="[a-zA-Z0-9-]{1,64}" maxLength={64} required autoFocus />{error && <div className="form-error">{error}</div>}</form></Modal>}
      {modal === "delete" && target && <Modal title="プロジェクトを削除しますか？" description="この操作はすぐに開始され、配下のすべてのリソースも削除対象になります。" onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-danger" type="button" onClick={() => void deleteProject()} disabled={busy}>{busy ? "削除を開始中…" : "プロジェクトを削除"}</button></>}><div className="delete-warning"><Trash2 size={16} /><span><strong>{target.name}</strong> のデプロイ、ボリューム、Ingress も非同期で削除されます。</span></div>{error && <div className="form-error">{error}</div>}</Modal>}
      {toast && <ToastNotice message={toast} onDismiss={() => setToast("")} />}
    </section>
  )
}

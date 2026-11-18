"use client"

import { useState } from "react"
import { api } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import { validateProjectName } from "@/lib/project-name"
import { Modal, ToastNotice } from "@/components/ui-kit"

/**
 * プロジェクトの設定の画面(名前の変更と、削除)。
 *
 * @param props 共通の引数と、変更したときに呼ぶ onChanged(プロジェクトの一覧を取り直す)。
 */
export function SettingsView({ project, nav, onChanged }: ProjectViewProps & { onChanged: () => void }) {
  const [name, setName] = useState(project.name)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)
  const path = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const editable = project.status === "active"
  const problem = validateProjectName(name.trim())

  /** 名前の変更を送る。成功したら、一覧を取り直す。 */
  async function rename() {
    setBusy(true)
    try {
      await api.patch(`${path}`, { name: name.trim() })
      onChanged()
      setToast({ message: "名前を変更しました", error: false })
    } catch (caught) {
      setToast({ message: caught instanceof Error ? caught.message : "名前を変更できませんでした", error: true })
    } finally {
      setBusy(false)
    }
  }

  /** 削除を頼む。受け付けられたら、プロジェクト一覧へ戻る。 */
  async function remove() {
    setBusy(true)
    try {
      await api.delete(path)
      setConfirming(false)
      onChanged()
      nav.go("projects")
    } catch (caught) {
      setConfirming(false)
      setToast({ message: caught instanceof Error ? caught.message : "削除できませんでした", error: true })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="ws" style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <h1 className="h1 disp">設定</h1>
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <h2 className="h2">プロジェクト名</h2>
        <label className="lbl">名前<input className="inp" value={name} onChange={(event) => setName(event.target.value)} maxLength={64} disabled={!editable} /></label>
        {name.trim() !== project.name && problem && <div className="err" role="alert">{problem}</div>}
        <div><button className="btn primary" type="button" onClick={() => void rename()} disabled={busy || !editable || name.trim() === project.name || problem !== null}>変更を保存</button></div>
        <dl className="kv" style={{ margin: 0 }}><div><dt>ID</dt><dd className="mono">{project.projectId}</dd></div></dl>
      </div>
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12, borderColor: "#FAD2CF" }}>
        <h2 className="h2" style={{ color: "var(--bad)" }}>プロジェクトの削除</h2>
        <div className="mut">デプロイ、Ingress、ボリューム(データごと)も、全て削除されます。元に戻せません。</div>
        <div><button className="btn danger" type="button" onClick={() => setConfirming(true)} disabled={busy || project.status === "deleting"}>プロジェクトを削除</button></div>
      </div>
      {confirming && (
        <Modal title="プロジェクトを削除しますか?" onClose={() => setConfirming(false)} footer={<><button className="btn" type="button" onClick={() => setConfirming(false)}>やめる</button><button className="btn warn" type="button" onClick={() => void remove()} disabled={busy}>削除する</button></>}>
          <div><b>{project.name}</b> と、配下の全てを削除します。</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => setToast(null)} />}
    </div>
  )
}

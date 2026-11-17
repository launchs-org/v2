"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { Database, HardDrive, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react"
import { api, authenticatedFetcher } from "@/lib/api"
import type { Project, QuotaResponse, Volume, VolumeListResponse } from "@/lib/types"
import { EmptyState, ErrorState, formattedDate, Modal, PageHeading, StatusPill, ToastNotice } from "@/components/ui-kit"

function CreateVolumeModal({ project, maxSizeMB, onClose, onCreated }: { project: Project; maxSizeMB?: number; onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState("")
  const [sizeGB, setSizeGB] = useState(1)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      await api.post(`/api/v1/projects/${encodeURIComponent(project.projectId)}/volumes`, { name: name.trim(), sizeMB: Math.round(sizeGB * 1024) })
      onCreated()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "ボリュームを作成できませんでした")
    } finally {
      setBusy(false)
    }
  }
  const maxGB = maxSizeMB ? maxSizeMB / 1024 : undefined
  return <Modal title="ボリュームを作成" description="永続ストレージ領域をプロジェクトに追加します。" onClose={onClose} footer={<><button className="btn btn-secondary" type="button" onClick={onClose}>キャンセル</button><button className="btn btn-primary" type="submit" form="volume-create-form" disabled={busy || !name.trim()}>{busy ? "作成中…" : <><Plus size={14} />作成する</>}</button></>}><form id="volume-create-form" className="stack-form" onSubmit={submit}><div><label className="form-label" htmlFor="volume-name">ボリューム名</label><input id="volume-name" className="form-control" value={name} onChange={(event) => setName(event.target.value)} pattern="[a-zA-Z0-9-]{1,64}" maxLength={64} placeholder="db-data" required autoFocus /></div><div><label className="form-label" htmlFor="volume-size">初期容量</label><div className="input-suffix-wrap"><input id="volume-size" className="form-control" type="number" min={1} step="0.5" max={maxGB} value={sizeGB} onChange={(event) => setSizeGB(Number(event.target.value))} required /><span>GB</span></div><p className="editor-help">最小 1 GB{maxGB ? ` · 上限 ${maxGB.toLocaleString()} GB` : ""}。後から容量を拡張できます。</p></div>{error && <div className="form-error">{error}</div>}</form></Modal>
}

function VolumeCard({ volume, projectId, maxSizeMB, onRefresh, onToast }: { volume: Volume; projectId: string; maxSizeMB?: number; onRefresh: () => void; onToast: (message: string, isError?: boolean) => void }) {
  const [sizeMB, setSizeMB] = useState(volume.pending_sizeMB ?? volume.sizeMB)
  const [name, setName] = useState(volume.name)
  const [modal, setModal] = useState<"rename" | "delete" | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { setSizeMB(volume.pending_sizeMB ?? volume.sizeMB) }, [volume.pending_sizeMB, volume.sizeMB])
  useEffect(() => { setName(volume.name) }, [volume.name])
  const base = `/api/v1/projects/${encodeURIComponent(projectId)}/volumes/${encodeURIComponent(volume.volumeId)}`
  const isPending = volume.pending_sizeMB !== null

  async function saveSize() {
    if (sizeMB <= volume.sizeMB) { onToast("容量は現在より大きい値を指定してください。縮小はできません。", true); return }
    if (maxSizeMB && sizeMB > maxSizeMB) { onToast(`アカウント上限の ${maxSizeMB.toLocaleString()} MB を超えています。`, true); return }
    setBusy(true)
    try {
      await api.patch(`${base}/size`, { sizeMB })
      onToast("容量変更を pending として保存しました")
      onRefresh()
    } catch (caught) { onToast(caught instanceof Error ? caught.message : "容量を変更できませんでした", true) }
    finally { setBusy(false) }
  }

  async function pendingAction(action: "apply" | "discard") {
    setBusy(true)
    try {
      await api.post(`${base}/${action}`)
      onToast(action === "apply" ? "ボリューム拡張を開始しました。状態は自動更新されます" : "容量変更の下書きを破棄しました")
      onRefresh()
    } catch (caught) { onToast(caught instanceof Error ? caught.message : "操作を完了できませんでした", true) }
    finally { setBusy(false) }
  }

  async function rename(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    try {
      await api.patch(base, { name: name.trim() })
      setModal(null)
      onToast("ボリューム名を変更しました")
      onRefresh()
    } catch (caught) { onToast(caught instanceof Error ? caught.message : "名前を変更できませんでした", true) }
    finally { setBusy(false) }
  }

  async function remove() {
    setBusy(true)
    try {
      await api.delete(base)
      setModal(null)
      onToast("ボリュームの削除を開始しました")
      onRefresh()
    } catch (caught) { onToast(caught instanceof Error ? caught.message : "ボリュームを削除できませんでした。マウント中ではないか確認してください。", true) }
    finally { setBusy(false) }
  }

  return <article className="resource-card volume-card">
    <div className="resource-card-top"><span className="resource-icon teal"><HardDrive size={17} /></span><div className="resource-title"><h3>{volume.name}</h3><code>{volume.volumeId}</code></div><StatusPill status={volume.status} /><div className="resource-menu-actions"><button className="icon-button" type="button" onClick={() => setModal("rename")} disabled={volume.status === "deleting"} title="名前を変更" aria-label={`${volume.name} の名前を変更`}><Pencil size={14} /></button><button className="icon-button danger-icon" type="button" onClick={() => setModal("delete")} disabled={volume.status === "deleting"} title="削除" aria-label={`${volume.name} を削除`}><Trash2 size={14} /></button></div></div>
    <div className="volume-size-row"><div><span className="small-label">現在の容量</span><strong>{(volume.sizeMB / 1024).toLocaleString(undefined, { maximumFractionDigits: 2 })} <small>GB</small></strong></div><div className="volume-size-arrow">→</div><div className={isPending ? "volume-pending" : ""}><span className="small-label">{isPending ? "適用後" : "変更後"}</span><strong>{((isPending ? volume.pending_sizeMB : sizeMB) as number / 1024).toLocaleString(undefined, { maximumFractionDigits: 2 })} <small>GB</small></strong></div></div>
    <div className="resize-form"><label className="form-label" htmlFor={`resize-${volume.volumeId}`}>容量を拡張 <span className="optional-label">GB</span></label><div className="resize-controls"><div className="input-suffix-wrap"><input id={`resize-${volume.volumeId}`} className="form-control" type="number" min={Math.ceil(volume.sizeMB / 1024)} step="0.5" max={maxSizeMB ? maxSizeMB / 1024 : undefined} value={sizeMB / 1024} onChange={(event) => setSizeMB(Math.round(Number(event.target.value) * 1024))} disabled={volume.status === "applying" || volume.status === "deleting"} /><span>GB</span></div><button className="btn btn-secondary btn-sm" type="button" onClick={() => void saveSize()} disabled={busy || volume.status === "applying" || sizeMB <= volume.sizeMB}>{busy ? "処理中…" : "下書きを保存"}</button></div></div>
    {isPending && <div className="pending-bar compact-pending"><div className="pending-copy"><span className="pending-dot" /><div><strong>容量変更が pending</strong><span>適用前に現在のボリュームは変更されません</span></div></div><div className="pending-actions"><button className="btn btn-secondary btn-sm" type="button" onClick={() => void pendingAction("discard")} disabled={busy}>破棄</button><button className="btn btn-primary btn-sm" type="button" onClick={() => void pendingAction("apply")} disabled={busy}><RefreshCw size={13} />Apply</button></div></div>}
    <div className="resource-card-foot"><span>{volume.createdAt ? `作成日時 · ${formattedDate(volume.createdAt)}` : "Persistent volume"}</span><span className="small-muted">ID {volume.volumeId.slice(0, 12)}</span></div>
    {modal === "rename" && <Modal title="ボリューム名を変更" description="名前の変更は即時反映されます。" onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-primary" type="submit" form={`rename-${volume.volumeId}`} disabled={busy || !name.trim()}>{busy ? "保存中…" : "変更を保存"}</button></>}><form id={`rename-${volume.volumeId}`} onSubmit={rename}><label className="form-label" htmlFor={`rename-name-${volume.volumeId}`}>新しい名前</label><input id={`rename-name-${volume.volumeId}`} className="form-control" value={name} onChange={(event) => setName(event.target.value)} pattern="[a-zA-Z0-9-]{1,64}" maxLength={64} required autoFocus /></form></Modal>}
    {modal === "delete" && <Modal title="ボリュームを削除しますか？" description="現在 Pod にマウント中のボリュームは削除できません。" onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-danger" type="button" onClick={() => void remove()} disabled={busy}>{busy ? "削除中…" : "削除する"}</button></>}><div className="delete-warning"><Database size={16} /><span><strong>{volume.name}</strong> · {(volume.sizeMB / 1024).toFixed(1)} GB</span></div></Modal>}
  </article>
}

export function VolumesView({ project, refreshInterval }: { project: Project; refreshInterval: number }) {
  const key = `/api/v1/projects/${encodeURIComponent(project.projectId)}/volumes?limit=100`
  const { data, error, mutate } = useSWR<VolumeListResponse>(key, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: quota } = useSWR<QuotaResponse>("/api/v1/quota", authenticatedFetcher, { refreshInterval: refreshInterval ? 15_000 : 0 })
  const [modal, setModal] = useState(false)
  const [toast, setToast] = useState("")
  const [toastError, setToastError] = useState(false)
  function notify(message: string, isError = false) { setToast(message); setToastError(isError) }
  const volumes = data?.volumes ?? []
  const total = volumes.reduce((sum, volume) => sum + volume.sizeMB, 0)
  return <section>
    <PageHeading eyebrow="PERSISTENT STORAGE" title="ボリューム管理" description="永続ボリュームを作成し、容量の拡張とデプロイへのマウントを管理します。" actions={<button className="btn btn-primary" type="button" onClick={() => setModal(true)}><Plus size={15} />ボリュームを作成</button>} />
    <div className="storage-summary"><div className="storage-summary-icon"><Database size={17} /></div><div className="storage-summary-copy"><span>アカウント使用容量</span><strong>{((quota?.volumeTotalMB.used ?? total) / 1024).toLocaleString(undefined, { maximumFractionDigits: 2 })} <small>GB</small><span className="storage-limit"> / {quota ? (quota.volumeTotalMB.limit / 1024).toLocaleString() : "—"} GB quota</span></strong></div><div className="storage-summary-meter"><span style={{ width: `${quota?.volumeTotalMB.limit ? Math.min(quota.volumeTotalMB.used / quota.volumeTotalMB.limit * 100, 100) : 0}%` }} /></div><span className="storage-count">{volumes.length} in project</span></div>
    {error && <ErrorState message="ボリューム一覧を取得できませんでした。" onRetry={() => void mutate()} />}
    {!data ? <div className="skeleton-cards"><div /><div /></div> : !volumes.length ? <div className="card"><EmptyState icon={<Database size={18} />} title="ボリュームはありません" description="永続データを保存するストレージを作成し、デプロイの Pod にマウントできます。" action={<button className="btn btn-primary" type="button" onClick={() => setModal(true)}><Plus size={14} />最初のボリュームを作成</button>} /></div> : <div className="volume-grid">{volumes.map((volume) => <VolumeCard key={volume.volumeId} volume={volume} projectId={project.projectId} maxSizeMB={quota?.maxVolumeSizeMB} onRefresh={() => { void mutate() }} onToast={notify} />)}</div>}
    <div className="info-banner storage-note"><Database size={15} /><span>容量拡張は <strong>pending → Apply</strong> の順に反映します。縮小はできません。デプロイ側の「ボリューム」タブから Pod へのマウントを設定できます。</span></div>
    {modal && <CreateVolumeModal project={project} maxSizeMB={quota?.maxVolumeSizeMB} onClose={() => setModal(false)} onCreated={() => { setModal(false); notify("ボリューム作成を受け付けました。状態は自動更新されます"); void mutate() }} />}
    {toast && <ToastNotice message={toast} type={toastError ? "error" : "success"} onDismiss={() => setToast("")} />}
  </section>
}

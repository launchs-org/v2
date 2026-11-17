"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import {
  AlertTriangle, Box, Check, ChevronRight, CirclePlus, Cpu, Database, Globe2, LoaderCircle,
  Plus, Rocket, Server, Settings2, Trash2, Wrench,
} from "lucide-react"
import { api, authenticatedFetcher } from "@/lib/api"
import type { BuildListResponse, DeployDetail, DeployListResponse, IngressRule, LogPodInfo, MountItem, Project, ServicePort } from "@/lib/types"
import { EmptyState, ErrorState, formattedDate, LoadingState, Modal, PageHeading, StatusPill, ToastNotice } from "@/components/ui-kit"
import { LogTail } from "@/components/log-tail"

type DeploymentsViewProps = { project: Project; refreshInterval: number }
type TabKey = "overview" | "config" | "service" | "storage" | "logs"

const tabs: { id: TabKey; label: string; icon: typeof Box }[] = [
  { id: "overview", label: "概要", icon: Box },
  { id: "config", label: "起動設定", icon: Settings2 },
  { id: "service", label: "Service", icon: Globe2 },
  { id: "storage", label: "ボリューム", icon: Database },
  { id: "logs", label: "ログ", icon: Server },
]

function DeployCreateModal({ project, imageRef: initialImageRef, onClose, onCreated }: { project: Project; imageRef: string; onClose: () => void; onCreated: (deployId: string) => void }) {
  const [imageRef, setImageRef] = useState(initialImageRef)
  const [spec, setSpec] = useState<"small" | "medium" | "large">("small")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      const result = await api.post<{ deployId: string }>("/api/v1/deploy", { projectId: project.projectId, imageRef: imageRef.trim(), spec })
      onCreated(result.deployId)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "デプロイを開始できませんでした")
    } finally {
      setBusy(false)
    }
  }
  return <Modal title="デプロイを作成" description="コンテナイメージとリソースサイズを指定して起動します。" onClose={onClose} footer={<><button className="btn btn-secondary" type="button" onClick={onClose}>キャンセル</button><button className="btn btn-primary" type="submit" form="deploy-create-form" disabled={busy || !imageRef.trim()}>{busy ? <><span className="loader" />作成中…</> : <><Rocket size={14} />デプロイを開始</>}</button></>}>
    <form id="deploy-create-form" onSubmit={submit} className="stack-form"><div><label className="form-label" htmlFor="deploy-image">イメージ参照</label><input id="deploy-image" className="form-control mono-input" value={imageRef} onChange={(event) => setImageRef(event.target.value)} placeholder="registry.example.com/app:latest" required autoFocus /><p className="editor-help">ビルド済みのコンテナイメージを指定してください。</p></div><div><label className="form-label" htmlFor="deploy-spec">リソースプラン</label><select id="deploy-spec" className="select-control" value={spec} onChange={(event) => setSpec(event.target.value as typeof spec)}><option value="small">Small · 共有ワークロード</option><option value="medium">Medium · 標準ワークロード</option><option value="large">Large · 高負荷ワークロード</option></select></div>{error && <div className="form-error">{error}</div>}</form>
  </Modal>
}

function BuildCreateModal({ project, onClose, onCreated }: { project: Project; onClose: () => void; onCreated: () => void }) {
  const [repoUrl, setRepoUrl] = useState("")
  const [ref, setRef] = useState("main")
  const [targetDirectory, setTargetDirectory] = useState("")
  const [method, setMethod] = useState<"dockerfile" | "railpack">("dockerfile")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      await api.post("/api/v1/build", { projectId: project.projectId, sourceType: "git", git: { repoUrl: repoUrl.trim(), ref: ref.trim() }, ...(targetDirectory.trim() ? { buildTargetDirectory: targetDirectory.trim() } : {}), buildMethod: method })
      onCreated()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "ビルドを開始できませんでした")
    } finally {
      setBusy(false)
    }
  }
  return <Modal title="Git からビルド" description="リポジトリからコンテナイメージを非同期でビルドします。" onClose={onClose} footer={<><button className="btn btn-secondary" type="button" onClick={onClose}>キャンセル</button><button className="btn btn-primary" form="build-create-form" type="submit" disabled={busy || !repoUrl.trim()}>{busy ? "受付中…" : <><Wrench size={14} />ビルドを開始</>}</button></>}>
    <form id="build-create-form" onSubmit={submit} className="stack-form"><div><label className="form-label" htmlFor="repo-url">Git リポジトリ URL</label><input id="repo-url" className="form-control mono-input" type="url" value={repoUrl} onChange={(event) => setRepoUrl(event.target.value)} placeholder="https://github.com/org/repository.git" required autoFocus /></div><div className="form-grid-two"><div><label className="form-label" htmlFor="git-ref">ブランチ / ref</label><input id="git-ref" className="form-control" value={ref} onChange={(event) => setRef(event.target.value)} required /></div><div><label className="form-label" htmlFor="build-method">ビルド方法</label><select id="build-method" className="select-control" value={method} onChange={(event) => setMethod(event.target.value as typeof method)}><option value="dockerfile">Dockerfile</option><option value="railpack">Railpack</option></select></div></div><div><label className="form-label" htmlFor="build-directory">対象ディレクトリ <span className="optional-label">任意</span></label><input id="build-directory" className="form-control mono-input" value={targetDirectory} onChange={(event) => setTargetDirectory(event.target.value)} placeholder="例: apps/api" /></div>{error && <div className="form-error">{error}</div>}</form>
  </Modal>
}

function DraftActions({ pending, disabled, onApply, onDiscard }: { pending: boolean; disabled: boolean; onApply: () => void; onDiscard: () => void }) {
  if (!pending) return null
  return <div className="pending-bar"><div className="pending-copy"><span className="pending-dot" /><div><strong>未反映の変更があります</strong><span>下書き保存済み · apply で反映を開始できます</span></div></div><div className="pending-actions"><button className="btn btn-secondary btn-sm" type="button" onClick={onDiscard} disabled={disabled}>破棄</button><button className="btn btn-primary btn-sm" type="button" onClick={onApply} disabled={disabled}><Check size={13} />Apply</button></div></div>
}

function DeployConfigPanel({ detail, refreshDetail, setToast }: { detail: DeployDetail; refreshDetail: () => void; setToast: (message: string, error?: boolean) => void }) {
  const config = detail.config
  const [envText, setEnvText] = useState("{}")
  const [command, setCommand] = useState("")
  const [argsText, setArgsText] = useState("")
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const pending = config.pending_env !== null || config.pending_command !== null || config.pending_args !== null
  useEffect(() => {
    if (!dirty) {
      setEnvText(JSON.stringify(config.pending_env ?? config.env ?? {}, null, 2))
      setCommand(config.pending_command ?? config.command ?? "")
      setArgsText((config.pending_args ?? config.args ?? []).join("\n"))
    }
  }, [config, dirty])

  async function save() {
    setBusy(true)
    try {
      const env = JSON.parse(envText) as unknown
      if (!env || Array.isArray(env) || typeof env !== "object" || Object.values(env).some((value) => typeof value !== "string")) throw new Error("環境変数は文字列値を持つ JSON オブジェクトで入力してください")
      await api.put(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/config`, { env, command: command.trim() || null, args: argsText.split("\n").map((value) => value.trim()).filter(Boolean) })
      setDirty(false)
      setToast("起動設定を下書きとして保存しました")
      refreshDetail()
    } catch (caught) {
      setToast(caught instanceof Error ? caught.message : "設定を保存できませんでした", true)
    } finally {
      setBusy(false)
    }
  }

  async function pendingAction(action: "apply" | "discard") {
    setBusy(true)
    try {
      await api.post(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/config/${action}`)
      setToast(action === "apply" ? "起動設定の反映を開始しました。状態は自動更新されます" : "起動設定の下書きを破棄しました")
      setDirty(false)
      refreshDetail()
    } catch (caught) {
      setToast(caught instanceof Error ? caught.message : "操作を完了できませんでした", true)
    } finally {
      setBusy(false)
    }
  }

  return <div className="detail-panel stack-form">
    <div className="panel-intro"><div><h3>コンテナ起動設定</h3><p>環境変数、起動コマンド、引数を編集できます。値は保存後もすぐには反映されません。</p></div><span className={pending ? "draft-label" : "current-label"}>{pending ? "DRAFT PENDING" : "CURRENT CONFIG"}</span></div>
    <div><label className="form-label" htmlFor="env-json">環境変数 <span className="optional-label">JSON</span></label><textarea id="env-json" className="form-control code-editor" spellCheck={false} value={envText} onChange={(event) => { setEnvText(event.target.value); setDirty(true) }} rows={7} placeholder={'{\n  "PORT": "8080"\n}'} /><p className="editor-help">値は画面上に表示されます。秘密情報を扱う場合はアクセス権限にご注意ください。</p></div>
    <div><label className="form-label" htmlFor="command">起動コマンド <span className="optional-label">任意</span></label><input id="command" className="form-control mono-input" value={command} onChange={(event) => { setCommand(event.target.value); setDirty(true) }} placeholder="/app/server" /></div>
    <div><label className="form-label" htmlFor="args">引数 <span className="optional-label">1行につき1つ</span></label><textarea id="args" className="form-control code-editor compact" spellCheck={false} value={argsText} onChange={(event) => { setArgsText(event.target.value); setDirty(true) }} rows={3} placeholder={'--config\n/app/config.yaml'} /></div>
    <div className="form-bottom-row"><span className="muted-inline">保存すると pending としてステージされます</span><button className="btn btn-secondary" type="button" onClick={() => void save()} disabled={busy || !dirty}>{busy ? "保存中…" : "下書きを保存"}</button></div>
    <DraftActions pending={pending} disabled={busy || detail.status === "applying" || detail.status === "deleting"} onApply={() => void pendingAction("apply")} onDiscard={() => void pendingAction("discard")} />
  </div>
}

function ServicePanel({ detail, refreshDetail, setToast }: { detail: DeployDetail; refreshDetail: () => void; setToast: (message: string, error?: boolean) => void }) {
  const initialPorts = detail.service?.pending_ports ?? detail.service?.ports ?? []
  const [ports, setPorts] = useState<ServicePort[]>(initialPorts)
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!dirty) setPorts(detail.service?.pending_ports ?? detail.service?.ports ?? []) }, [detail.service, dirty])
  const pending = detail.service?.pending_ports !== null && detail.service !== null

  function updatePort(index: number, key: keyof ServicePort, value: string) {
    setPorts((current) => current.map((port, portIndex) => portIndex === index ? { ...port, [key]: key === "port" ? Number(value) : value } : port))
    setDirty(true)
  }

  async function savePorts() {
    if (!ports.length || ports.some((port) => !port.name.trim() || port.port < 1 || port.port > 65535)) {
      setToast("有効なポート名と 1〜65535 のポート番号を入力してください", true)
      return
    }
    setBusy(true)
    try {
      if (detail.service) await api.patch(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/service`, { ports })
      else await api.post(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/service`, { ports })
      setDirty(false)
      setToast(detail.service ? "Service の変更を下書きとして保存しました" : "Service を作成しました")
      refreshDetail()
    } catch (caught) {
      setToast(caught instanceof Error ? caught.message : "Service を保存できませんでした", true)
    } finally {
      setBusy(false)
    }
  }

  async function pendingAction(action: "apply" | "discard") {
    setBusy(true)
    try {
      await api.post(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/service/${action}`)
      setToast(action === "apply" ? "Service の反映を開始しました" : "Service の下書きを破棄しました")
      setDirty(false)
      refreshDetail()
    } catch (caught) {
      setToast(caught instanceof Error ? caught.message : "操作を完了できませんでした", true)
    } finally {
      setBusy(false)
    }
  }

  return <div className="detail-panel stack-form">
    <div className="panel-intro"><div><h3>Service ポート</h3><p>クラスタ内に公開するポートを定義します。Ingress から参照されている Service は削除できません。</p></div>{detail.service && <StatusPill status={detail.service.status} />}</div>
    {!detail.service && <div className="info-banner"><Globe2 size={15} />Service を作成すると、プロジェクト内 DNS と ClusterIP が払い出されます。</div>}
    {detail.service?.dnsName && <div className="service-endpoint"><span>CLUSTER DNS</span><code>{detail.service.dnsName}</code>{detail.service.clusterIP && <small>Cluster IP · {detail.service.clusterIP}</small>}</div>}
    <div className="port-list-head"><span>名前</span><span>ポート</span><span>プロトコル</span><span className="sr-only">削除</span></div>
    {ports.map((port, index) => <div className="port-row" key={`${index}-${port.name}`}><input className="form-control" aria-label={`ポート ${index + 1} の名前`} value={port.name} onChange={(event) => updatePort(index, "name", event.target.value)} placeholder="web" /><input className="form-control mono-input" type="number" min={1} max={65535} aria-label={`ポート ${index + 1} の番号`} value={port.port} onChange={(event) => updatePort(index, "port", event.target.value)} /><select className="select-control" aria-label={`ポート ${index + 1} のプロトコル`} value={port.protocol} onChange={(event) => updatePort(index, "protocol", event.target.value)}><option value="tcp">TCP</option><option value="udp">UDP</option></select><button className="icon-button danger-icon" type="button" onClick={() => { setPorts((current) => current.filter((_, portIndex) => portIndex !== index)); setDirty(true) }} aria-label={`ポート ${index + 1} を削除`}><Trash2 size={14} /></button></div>)}
    <button className="btn btn-ghost btn-sm add-row-button" type="button" onClick={() => { setPorts((current) => [...current, { name: "web", port: 8080, protocol: "tcp" }]); setDirty(true) }}><Plus size={14} />ポートを追加</button>
    <div className="form-bottom-row"><span className="muted-inline">1〜65535 · name は英小文字、数字、ハイフン</span><button className="btn btn-secondary" type="button" onClick={() => void savePorts()} disabled={busy || !dirty}>{busy ? "保存中…" : detail.service ? "下書きを保存" : "Service を作成"}</button></div>
    {detail.service && <DraftActions pending={pending} disabled={busy || detail.service.status === "deleting"} onApply={() => void pendingAction("apply")} onDiscard={() => void pendingAction("discard")} />}
  </div>
}

function StoragePanel({ detail, project, refreshInterval, refreshDetail, setToast }: { detail: DeployDetail; project: Project; refreshInterval: number; refreshDetail: () => void; setToast: (message: string, error?: boolean) => void }) {
  const { data: volumeData } = useSWR<{ volumes: { volumeId: string; name: string; status: string; sizeMB: number; pending_sizeMB: number | null }[] }>(`/api/v1/projects/${encodeURIComponent(project.projectId)}/volumes?limit=100`, authenticatedFetcher, { refreshInterval })
  const activeVolumes = (volumeData?.volumes ?? []).filter((volume) => volume.status !== "deleting")
  const [mounts, setMounts] = useState<MountItem[]>(detail.volumes.pending_mounts ?? detail.volumes.mounts ?? [])
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!dirty) setMounts(detail.volumes.pending_mounts ?? detail.volumes.mounts ?? []) }, [detail.volumes, dirty])
  const pending = detail.volumes.pending_mounts !== null

  async function saveMounts() {
    if (mounts.some((mount) => !mount.volumeId || !mount.mountPath.startsWith("/"))) {
      setToast("ボリュームと / から始まるマウントパスを指定してください", true)
      return
    }
    setBusy(true)
    try {
      await api.put(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/mounts`, { mounts })
      setDirty(false)
      setToast("マウント設定を下書きとして保存しました")
      refreshDetail()
    } catch (caught) {
      setToast(caught instanceof Error ? caught.message : "マウント設定を保存できませんでした", true)
    } finally {
      setBusy(false)
    }
  }

  async function pendingAction(action: "apply" | "discard") {
    setBusy(true)
    try {
      await api.post(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/mounts/${action}`)
      setToast(action === "apply" ? "マウント設定の反映を開始しました" : "マウント設定の下書きを破棄しました")
      setDirty(false)
      refreshDetail()
    } catch (caught) {
      setToast(caught instanceof Error ? caught.message : "操作を完了できませんでした", true)
    } finally {
      setBusy(false)
    }
  }

  return <div className="detail-panel stack-form">
    <div className="panel-intro"><div><h3>ボリュームマウント</h3><p>このデプロイの Pod に接続する永続ボリュームを設定します。</p></div><span className={pending ? "draft-label" : "current-label"}>{pending ? "DRAFT PENDING" : `${mounts.length} MOUNTS`}</span></div>
    {!activeVolumes.length && <div className="info-banner"><Database size={15} />マウントするボリュームがありません。先にボリューム画面で作成してください。</div>}
    {mounts.map((mount, index) => <div className="mount-row" key={`${index}-${mount.mountPath}`}><div><label className="form-label" htmlFor={`volume-${index}`}>ボリューム</label><select id={`volume-${index}`} className="select-control" value={mount.volumeId} onChange={(event) => { setMounts((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, volumeId: event.target.value } : item)); setDirty(true) }}><option value="">ボリュームを選択</option>{activeVolumes.map((volume) => <option key={volume.volumeId} value={volume.volumeId}>{volume.name} · {(volume.sizeMB / 1024).toFixed(1)} GB</option>)}</select></div><div><label className="form-label" htmlFor={`mount-${index}`}>マウントパス</label><input id={`mount-${index}`} className="form-control mono-input" value={mount.mountPath} onChange={(event) => { setMounts((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, mountPath: event.target.value } : item)); setDirty(true) }} placeholder="/data" /></div><button className="icon-button danger-icon mount-remove" type="button" onClick={() => { setMounts((current) => current.filter((_, itemIndex) => itemIndex !== index)); setDirty(true) }} aria-label={`マウント ${index + 1} を削除`}><Trash2 size={14} /></button></div>)}
    <div className="row-control-actions"><button className="btn btn-ghost btn-sm" type="button" onClick={() => { setMounts((current) => [...current, { volumeId: activeVolumes[0]?.volumeId ?? "", mountPath: "/data" }]); setDirty(true) }} disabled={!activeVolumes.length}><Plus size={14} />マウントを追加</button><button className="btn btn-secondary" type="button" onClick={() => void saveMounts()} disabled={busy || !dirty}>{busy ? "保存中…" : "下書きを保存"}</button></div>
    <DraftActions pending={pending} disabled={busy || detail.status === "applying" || detail.status === "deleting"} onApply={() => void pendingAction("apply")} onDiscard={() => void pendingAction("discard")} />
  </div>
}

export function DeploymentsView({ project, refreshInterval }: DeploymentsViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: deployData, error: deployError, mutate: refreshDeploys } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: buildData, mutate: refreshBuilds } = useSWR<BuildListResponse>(`${base}/builds?limit=30`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [selectedId, setSelectedId] = useState("")
  const [tab, setTab] = useState<TabKey>("overview")
  const [modal, setModal] = useState<"deploy" | "build" | "delete" | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<DeployDetail | null>(null)
  const [selectedImage, setSelectedImage] = useState("")
  const [scale, setScale] = useState<number | null>(null)
  const [scaleBusy, setScaleBusy] = useState(false)
  const [toast, setToast] = useState("")
  const [toastError, setToastError] = useState(false)
  const deploys = deployData?.deploys ?? []
  const activeDeployId = deploys.some((deploy) => deploy.deployId === selectedId) ? selectedId : deploys[0]?.deployId ?? ""
  const { data: detail, error: detailError, mutate: refreshDetail } = useSWR<DeployDetail>(activeDeployId ? `/api/v1/deploy/${encodeURIComponent(activeDeployId)}` : null, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  useEffect(() => { if (detail && scale === null) setScale(detail.replicas) }, [detail, scale])

  function showToast(message: string, isError = false) {
    setToast(message)
    setToastError(isError)
  }

  async function updateScale() {
    if (!detail || scale === null || scale === detail.replicas) return
    setScaleBusy(true)
    try {
      await api.post(`/api/v1/deploy/${encodeURIComponent(detail.deployId)}/scale`, { replicas: scale })
      showToast("スケール要求を受け付けました。反映状態は自動更新されます")
      void refreshDetail()
      void refreshDeploys()
    } catch (caught) {
      showToast(caught instanceof Error ? caught.message : "スケールを変更できませんでした", true)
    } finally {
      setScaleBusy(false)
    }
  }

  async function deleteDeployment() {
    if (!deleteTarget) return
    try {
      await api.delete(`/api/v1/deploy/${encodeURIComponent(deleteTarget.deployId)}`)
      setModal(null)
      showToast("デプロイの削除を開始しました")
      void refreshDeploys()
    } catch (caught) {
      showToast(caught instanceof Error ? caught.message : "デプロイを削除できませんでした", true)
    }
  }

  const builds = buildData?.builds ?? []
  return <section>
    <PageHeading eyebrow="WORKLOADS" title="デプロイ管理" description={`${project.name} のコンテナ、スケール、起動設定、Service を管理します。`} actions={<><button className="btn btn-secondary" type="button" onClick={() => setModal("build")}><Wrench size={14} />Git からビルド</button><button className="btn btn-primary" type="button" onClick={() => { setSelectedImage(""); setModal("deploy") }}><Plus size={15} />新規デプロイ</button></>} />
    {deployError && <ErrorState message="デプロイ一覧を取得できませんでした。" onRetry={() => void refreshDeploys()} />}
    <div className="deploy-layout">
      <aside className="card deploy-list-card">
        <div className="list-heading"><div><strong>デプロイ</strong><span>{deployData ? `${deploys.length} 件` : "読み込み中"}</span></div><button className="icon-button" type="button" onClick={() => void refreshDeploys()} title="一覧を更新" aria-label="デプロイ一覧を更新"><LoaderCircle size={15} className={deployData ? "" : "spin-icon"} /></button></div>
        {!deployData ? <div className="list-loading"><span className="loader" />取得中</div> : deploys.length === 0 ? <EmptyState icon={<Box size={16} />} title="デプロイなし" description="既存イメージまたは Git リポジトリから作成できます。" action={<button className="btn btn-secondary btn-sm" type="button" onClick={() => setModal("deploy")}><Plus size={13} />デプロイを作成</button>} /> : <div className="deploy-list">{deploys.map((deploy) => <button className={`deploy-list-item ${activeDeployId === deploy.deployId ? "selected" : ""}`} key={deploy.deployId} type="button" onClick={() => { setSelectedId(deploy.deployId); setScale(null) }}><span className="deploy-list-icon"><Box size={15} /></span><span className="deploy-list-copy"><strong>{deploy.imageRef.split("/").pop()}</strong><small>{deploy.deployId.slice(0, 20)} · {deploy.replicas} replicas</small><StatusPill status={deploy.status} /></span><ChevronRight size={14} className="deploy-chevron" /></button>)}</div>}
      </aside>
      <div className="deploy-detail-column">
        {activeDeployId && !detail && !detailError && <div className="card detail-loading"><LoadingState label="デプロイ詳細を取得しています" /></div>}
        {detailError && <div className="card"><ErrorState message="デプロイ詳細を取得できませんでした。" onRetry={() => void refreshDetail()} /></div>}
        {detail && <div className="card deploy-detail-card">
          <div className="deploy-detail-header"><div className="deploy-title"><span className="deploy-detail-icon"><Box size={18} /></span><div><div className="deploy-name-row"><h2>{detail.imageRef.split("/").pop()}</h2><StatusPill status={detail.status} /></div><p><code>{detail.deployId}</code><span>Created {formattedDate(detail.createdAt)}</span></p></div></div><button className="btn btn-danger-outline btn-sm" type="button" onClick={() => { setDeleteTarget(detail); setModal("delete") }} disabled={detail.status === "deleting" || detail.status === "applying"}><Trash2 size={13} />削除</button></div>
          <div className="deploy-info-grid"><div><span>IMAGE</span><code title={detail.imageRef}>{detail.imageRef}</code></div><div><span>SPEC</span><strong>{detail.spec.toUpperCase()}</strong></div><div><span>READY PODS</span><strong>{detail.pods.filter((pod) => pod.status === "running").length} / {detail.replicas}</strong></div></div>
          <div className="detail-tabs" role="tablist" aria-label="デプロイ設定"><div className="detail-tab-list">{tabs.map(({ id, label, icon: Icon }) => <button className={`detail-tab ${tab === id ? "active" : ""}`} key={id} type="button" role="tab" aria-selected={tab === id} onClick={() => setTab(id)}><Icon size={14} />{label}{id === "config" && (detail.config.pending_env !== null || detail.config.pending_command !== null || detail.config.pending_args !== null) && <span className="tab-pending-dot" />}{id === "storage" && detail.volumes.pending_mounts !== null && <span className="tab-pending-dot" />}</button>)}</div></div>
          <div className="tab-content" role="tabpanel">
            {tab === "overview" && <div className="detail-panel"><div className="detail-subheading"><div><h3>スケーリング</h3><p>希望レプリカ数を 0〜5 の範囲で設定します。</p></div><span className="scale-readout">{scale ?? detail.replicas}<small> / 5</small></span></div><div className="scale-control"><span>0</span><input type="range" min={0} max={5} step={1} value={scale ?? detail.replicas} onChange={(event) => setScale(Number(event.target.value))} aria-label="レプリカ数" /><span>5</span></div><div className="form-bottom-row"><span className="muted-inline">スケール変更は非同期で反映されます</span><button className="btn btn-secondary" type="button" onClick={() => void updateScale()} disabled={scaleBusy || scale === null || scale === detail.replicas || ["applying", "deleting", "scaling"].includes(detail.status)}>{scaleBusy ? "要求中…" : "スケールを適用"}</button></div><div className="pod-section"><div className="pod-section-head"><div><h3>Pods</h3><p>稼働中のコンテナ状態</p></div><span className="small-muted">{detail.pods.length} pods</span></div>{detail.pods.length ? <div className="pod-grid">{detail.pods.map((pod) => <div className="pod-card" key={pod.podId}><span className="pod-status-dot" data-status={pod.status} /><span><strong>{pod.podId.slice(0, 24)}</strong><small>{pod.status}</small></span></div>)}</div> : <div className="empty-inline">Pod 情報はまだありません。デプロイ状態を自動更新しています。</div>}</div></div>}
            {tab === "config" && <DeployConfigPanel detail={detail} refreshDetail={() => { void refreshDetail() }} setToast={showToast} />}
            {tab === "service" && <ServicePanel detail={detail} refreshDetail={() => { void refreshDetail() }} setToast={showToast} />}
            {tab === "storage" && <StoragePanel detail={detail} project={project} refreshInterval={refreshInterval} refreshDetail={() => { void refreshDetail() }} setToast={showToast} />}
            {tab === "logs" && <LogTail key={`${detail.deployId}:${detail.logPods[0]?.podId ?? "none"}`} deployId={detail.deployId} pods={detail.logPods as LogPodInfo[]} refreshInterval={refreshInterval} />}
          </div>
        </div>}
        {!activeDeployId && deployData && <div className="card empty-detail"><EmptyState icon={<Box size={18} />} title="デプロイを選択" description="一覧からデプロイを選ぶと、状態や設定を確認できます。" /></div>}
      </div>
    </div>

    <div className="card build-history-card"><div className="card-heading"><div><h2>ビルド履歴</h2><p>Git ソースから作成したイメージ</p></div><button className="btn btn-ghost btn-sm" type="button" onClick={() => setModal("build")}><Plus size={14} />ビルドを開始</button></div>{!buildData ? <div className="skeleton-row"><span /><span /><span /></div> : !builds.length ? <div className="empty-inline">ビルド履歴はありません。Git リポジトリからコンテナイメージをビルドできます。</div> : <div className="build-list">{builds.slice(0, 6).map((build) => <div className="build-row" key={build.buildId}><span className="build-row-icon"><Wrench size={14} /></span><div className="build-row-copy"><strong>{build.buildId}</strong><code>{build.imageRef}</code></div><StatusPill status={build.status} /><span className="small-muted">{formattedDate(build.createdAt)}</span>{build.status === "success" && <button className="btn btn-secondary btn-sm" type="button" onClick={() => { setSelectedImage(build.imageRef); setModal("deploy") }}>デプロイに使用</button>}</div>)}</div>}</div>

    {modal === "deploy" && <DeployCreateModal project={project} imageRef={selectedImage} onClose={() => setModal(null)} onCreated={(deployId) => { setModal(null); setSelectedId(deployId); setScale(null); showToast("デプロイを受け付けました。作成状態は自動更新されます"); void refreshDeploys() }} />}
    {modal === "build" && <BuildCreateModal project={project} onClose={() => setModal(null)} onCreated={() => { setModal(null); showToast("ビルドを受け付けました。進行状況は自動更新されます"); void refreshBuilds() }} />}
    {modal === "delete" && deleteTarget && <Modal title="デプロイを削除しますか？" description="関連する Pod と Service を削除します。ボリュームは保持されます。" onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-danger" type="button" onClick={() => void deleteDeployment()}>デプロイを削除</button></>}><div className="delete-warning"><AlertTriangle size={16} /><span><strong>{deleteTarget.deployId.slice(0, 28)}</strong> の Pod / Service が削除されます。</span></div></Modal>}
    {toast && <ToastNotice message={toast} type={toastError ? "error" : "success"} onDismiss={() => setToast("")} />}
  </section>
}

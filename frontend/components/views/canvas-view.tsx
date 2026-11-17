"use client"

import { useMemo, useState, type FormEvent } from "react"
import { api } from "@/lib/api"
import { buildCanvas } from "@/lib/canvas-layout"
import type { ProjectViewProps, Selection } from "@/lib/nav"
import { usePending } from "@/lib/pending"
import { validateProjectName } from "@/lib/project-name"
import { Canvas } from "@/components/canvas"
import { KindIcon, type Kind } from "@/components/kind-icon"
import { DeployPanel } from "@/components/panels/deploy-panel"
import { IngressPanel } from "@/components/panels/ingress-panel"
import { VolumePanel } from "@/components/panels/volume-panel"
import { ResizeHandle, usePanelWidth } from "@/components/panels/resize-handle"
import { DeployWizard } from "@/components/wizard/deploy-wizard"
import { ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"
import type { DeployTab } from "@/lib/nav"

type CanvasViewProps = ProjectViewProps & {
  /** いま、右のパネルで開いているカード。 */
  selection: Selection | null
  /** deploy のパネルのタブ。 */
  deployTab: DeployTab
  onDeployTabChange: (tab: DeployTab) => void
  /** ウィザードが開いているか。 */
  wizardOpen: boolean
  onWizardClose: () => void
}

/** 「+ 追加」メニューの項目。 */
const ADD_ITEMS: { key: "deploy" | "volume" | "ingress"; kind: Kind; title: string; description: string }[] = [
  { key: "deploy", kind: "deploy", title: "デプロイ", description: "イメージから、アプリを起動する" },
  { key: "volume", kind: "volume", title: "ボリューム", description: "消えないデータの置き場所" },
  { key: "ingress", kind: "ingress", title: "Ingress", description: "インターネットに公開する入口" },
]

/**
 * プロジェクトの「構成図」(メインの画面)。画面いっぱいのキャンバスに、ingress → deploy → volume のつながりを出す。
 * カードを押すと、右にパネルが開く。右上の「+ 追加」から、デプロイ(ウィザード)、ボリューム、Ingress を作る。
 *
 * @param props 共通の引数と、選択・タブ・ウィザードの状態。
 */
export function CanvasView(props: CanvasViewProps) {
  const { project, refreshInterval, nav, selection } = props
  const pending = usePending(project, refreshInterval)
  const [menuOpen, setMenuOpen] = useState(false)
  // 右のパネルの幅(ドラッグで変えられ、覚えておく)。
  const [panelWidth, setPanelWidth] = usePanelWidth()
  const [volumeModal, setVolumeModal] = useState(false)
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)
  const model = useMemo(() => buildCanvas({ deploys: pending.deployDetails, ingresses: pending.ingressDetails, volumes: pending.volumes }), [pending.deployDetails, pending.ingressDetails, pending.volumes])
  // 「読み込み中」の表示は、まだ何も出ていない最初のときだけ。取り直している間は、前のカードを出し続ける(ボリュームを作ったときなどに、図が消えないように)。
  const firstLoad = pending.loading && model.nodes.length === 0
  const empty = !pending.loading && model.nodes.length === 0

  /** Ingress を作る(ホスト名は自動。作ったら、そのパネルを開く)。 */
  async function createIngress() {
    setMenuOpen(false)
    setBusy(true)
    try {
      const created = await api.post<{ ingressId: string }>(`/api/v1/projects/${encodeURIComponent(project.projectId)}/ingresses`)
      await pending.refresh()
      nav.select({ kind: "ingress", id: created.ingressId })
    } catch (caught) {
      setToast({ message: caught instanceof Error ? caught.message : "Ingress を作成できませんでした", error: true })
    } finally {
      setBusy(false)
    }
  }

  /** 「+ 追加」の項目を選んだとき。 */
  function pick(key: "deploy" | "volume" | "ingress") {
    setMenuOpen(false)
    if (key === "deploy") nav.openWizard()
    else if (key === "volume") setVolumeModal(true)
    else void createIngress()
  }

  return (
    <div className="stage">
      <div className="canvas-wrap">
        {firstLoad && <div style={{ position: "absolute", inset: 0, display: "grid", placeItems: "center" }}><LoadingState label="構成を読み込んでいます" /></div>}
        {!firstLoad && !empty && <Canvas model={model} selection={selection} onSelect={nav.select} onBackgroundClick={nav.closePanel} />}
        {empty && (
          <div style={{ position: "absolute", inset: 0, display: "grid", placeItems: "center", padding: 24 }}>
            <div className="card pad" style={{ maxWidth: 460, textAlign: "center", display: "flex", flexDirection: "column", gap: 14, alignItems: "center", boxShadow: "0 10px 30px rgba(32,33,36,.08)" }}>
              <KindIcon kind="deploy" size={52} />
              <h2 className="h2" style={{ fontSize: 20 }}>最初のデプロイを作りましょう</h2>
              <div className="mut">イメージ名を入れるだけで、アプリを起動して、インターネットに公開できます。</div>
              <button className="btn primary" type="button" style={{ minHeight: 46, padding: "0 28px", fontSize: 15 }} onClick={nav.openWizard}>+ デプロイを追加</button>
            </div>
          </div>
        )}
        <div className="canvas-add">
          <button className="btn primary" type="button" onClick={() => setMenuOpen((current) => !current)} aria-expanded={menuOpen} disabled={busy}>+ 追加</button>
          {menuOpen && (
            <div className="addmenu" role="menu">
              {ADD_ITEMS.map((item) => <button key={item.key} type="button" role="menuitem" onClick={() => pick(item.key)}><KindIcon kind={item.kind} size={34} /><span><div className="t">{item.title}</div><div className="d">{item.description}</div></span></button>)}
            </div>
          )}
        </div>
      </div>

      {selection && (
        <aside className="panel" aria-label="詳細" style={{ width: `min(${panelWidth}px, 100%)` }}>
          <ResizeHandle width={panelWidth} onChange={setPanelWidth} />
          {selection.kind === "deploy" && <DeployPanel project={project} refreshInterval={refreshInterval} nav={nav} deployId={selection.id} tab={props.deployTab} onTabChange={props.onDeployTabChange} onClose={nav.closePanel} />}
          {selection.kind === "ingress" && <IngressPanel project={project} refreshInterval={refreshInterval} nav={nav} ingressId={selection.id} onClose={nav.closePanel} />}
          {selection.kind === "volume" && <VolumePanel project={project} refreshInterval={refreshInterval} nav={nav} volumeId={selection.id} onClose={nav.closePanel} />}
        </aside>
      )}

      {props.wizardOpen && <DeployWizard project={project} nav={nav} onClose={props.onWizardClose} />}
      {volumeModal && <CreateVolumeModal projectId={project.projectId} onClose={() => setVolumeModal(false)} onCreated={async (volumeId) => { setVolumeModal(false); await pending.refresh(); nav.select({ kind: "volume", id: volumeId }) }} />}
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => setToast(null)} />}
    </div>
  )
}

/**
 * ボリュームを作るモーダル(名前と大きさ)。
 *
 * @param props.projectId プロジェクトの ID。
 * @param props.onClose 閉じるとき。
 * @param props.onCreated 作れたとき(ボリュームの ID を渡す)。
 */
function CreateVolumeModal({ projectId, onClose, onCreated }: { projectId: string; onClose: () => void; onCreated: (volumeId: string) => Promise<void> }) {
  const [name, setName] = useState("")
  const [size, setSize] = useState("1024")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  /**
   * 送信されたとき: 検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // 手元で検査してから送る(サーバーも確かめる)。
    const problem = validateProjectName(name.trim())
    const sizeMB = Number(size)
    if (problem) return setError(problem)
    if (!Number.isInteger(sizeMB) || sizeMB < 1) return setError("大きさは、1 以上の整数(MB)で入力してください。")
    setBusy(true)
    setError("")
    try {
      const created = await api.post<{ volumeId: string }>(`/api/v1/projects/${encodeURIComponent(projectId)}/volumes`, { name: name.trim(), sizeMB })
      await onCreated(created.volumeId)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "ボリュームを作成できませんでした")
      setBusy(false)
    }
  }

  return (
    <Modal title="ボリュームを作る" description="消えないデータの置き場所です。デプロイを消しても残ります。" onClose={onClose} footer={<><button className="btn" type="button" onClick={onClose}>キャンセル</button><button className="btn primary" type="submit" form="volume-form" disabled={busy}>作成</button></>}>
      <form id="volume-form" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <label className="lbl">名前<input className="inp" value={name} onChange={(event) => setName(event.target.value)} placeholder="db-data" maxLength={64} autoFocus /></label>
        <label className="lbl">大きさ(MB)<input className="inp mono" inputMode="numeric" value={size} onChange={(event) => setSize(event.target.value)} /></label>
        {error && <ErrorState message={error} />}
      </form>
    </Modal>
  )
}

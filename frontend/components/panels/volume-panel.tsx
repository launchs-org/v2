"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import type { QuotaResponse, Volume } from "@/lib/types"
import { ApplyBar, Badge, LoadingState, Modal, ToastNotice, isBusyStatus } from "@/components/ui-kit"
import { PanelFrame } from "@/components/panels/panel-frame"

/** quota が取れないときの、ボリューム 1 つの大きさの上限(MB)。 */
const FALLBACK_MAX_MB = 5120

/**
 * 大きさ(MB)を、読みやすい文字にする。
 *
 * @param megabytes 大きさ(MB)。
 * @returns 1024 以上なら GB、未満なら MB。
 */
function sizeText(megabytes: number): string {
  // 1024MB 以上は、GB で出す。
  return megabytes >= 1024 ? `${Number.isInteger(megabytes / 1024) ? megabytes / 1024 : (megabytes / 1024).toFixed(1)} GB` : `${megabytes} MB`
}

/**
 * ボリュームの右パネル(容量と拡大、反映待ち、名前の変更、マウント先、削除)。
 *
 * 処理の流れ:
 *  1. ボリュームの詳細(マウント中の deploy を含む)と、容量の上限を取る。
 *  2. 拡大は、保存(未適用)→ 反映の順で行う(縮小はできない)。
 *  3. ボリュームが無くなった(削除が終わった)ら、パネルを閉じる。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.nav 画面を移る操作(マウント先の deploy を開く)。
 * @param props.volumeId ボリュームの ID。
 * @param props.onClose パネルを閉じるとき。
 */
export function VolumePanel({ project, refreshInterval, nav, volumeId, onClose }: ProjectViewProps & { volumeId: string; onClose: () => void }) {
  const path = `/api/v1/projects/${encodeURIComponent(project.projectId)}/volumes/${encodeURIComponent(volumeId)}`
  const { data: volume, error, mutate } = useSWR<Volume>(path, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: quota } = useSWR<QuotaResponse>("/api/v1/quota", authenticatedFetcher)
  const maxMb = quota?.maxVolumeSizeMB ?? FALLBACK_MAX_MB
  const [size, setSize] = useState("")
  const [name, setName] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState("")
  const [confirming, setConfirming] = useState(false)
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)

  /**
   * 通知を出す。
   *
   * @param message 本文。
   * @param isError 失敗なら true。
   */
  function notify(message: string, isError = false) { setToast({ message, error: isError }) }

  /**
   * 操作を実行し、終わったら取り直す(失敗は通知する)。
   *
   * @param action 実行する処理。
   */
  async function run(action: () => Promise<void>) {
    // 実行中の印を付け、成功なら取り直し、失敗なら通知する。
    setBusy(true)
    try { await action(); await mutate() } catch (caught) { notify(caught instanceof Error ? caught.message : "操作に失敗しました", true) } finally { setBusy(false) }
  }

  // 削除が終わって、ボリュームが無くなったら(取れなくなったら)、パネルを閉じる。
  // 取れなくなったら(削除が終わった、など)、パネルを閉じる(描画の途中ではなく、描画の後に閉じる)。
  useEffect(() => { if (error && !volume) onClose() }, [error, volume, onClose])
  if (!volume) return <PanelFrame kind="volume" title={volumeId} onClose={onClose}><LoadingState /></PanelFrame>
  const mounted = volume.mountedBy ?? []
  const locked = isBusyStatus(volume.status)

  return (
    <>
      <PanelFrame kind="volume" title={volume.name} subtitle={volume.volumeId} badge={<Badge status={volume.status} />} onClose={onClose}>
        {/* 容量と、拡大。 */}
        <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <div className="row" style={{ justifyContent: "space-between", alignItems: "baseline" }}><span className="mut">容量</span><span className="disp" style={{ fontSize: 24 }}>{sizeText(volume.sizeMB)}</span></div>
          {volume.pending_sizeMB !== null && <ApplyBar title={`拡張待ち: ${sizeText(volume.sizeMB)} → ${sizeText(volume.pending_sizeMB)}`} busy={busy || locked} onDiscard={() => void run(async () => { await api.post(`${path}/discard`); notify("破棄しました") })} onApply={() => void run(async () => { await api.post(`${path}/apply`); notify("拡張を開始しました") })} />}
          <div className="row" style={{ gap: 6, flexWrap: "nowrap" }}>
            <input className="inp mono" style={{ width: 130, minHeight: 36 }} type="number" value={size} onChange={(event) => { setSize(event.target.value); setProblem("") }} disabled={locked} placeholder={String(volume.sizeMB)} aria-label="新しい容量 (MB)" />
            <span className="faint mono">MB</span>
            <button className="btn sm" type="button" disabled={locked || busy || !size} onClick={() => {
              // 拡大だけ許す(縮小はできない)。上限も守る。
              const next = Number(size)
              if (!Number.isInteger(next) || next <= volume.sizeMB) { setProblem(`現在(${volume.sizeMB} MB)より大きい整数にしてください。縮小はできません。`); return }
              if (next > maxMb) { setProblem(`上限は ${maxMb} MB です。`); return }
              void run(async () => { await api.patch(`${path}/size`, { sizeMB: next }); setSize(""); notify("拡張を保存しました(未適用)") })
            }}>拡張を保存</button>
          </div>
          <div className="faint" style={{ fontSize: 12.5 }}>容量は、増やすだけできます(最大 {maxMb} MB)。</div>
          {problem && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{problem}</div>}
        </div>
        {/* 名前の変更。 */}
        <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <h2 className="h2">名前</h2>
          <div className="row" style={{ flexWrap: "nowrap", gap: 8 }}>
            <input className="inp" value={name ?? volume.name} onChange={(event) => setName(event.target.value)} disabled={locked} aria-label="名前" />
            <button className="btn sm" type="button" disabled={locked || busy || name === null || name === volume.name || !/^[A-Za-z0-9-]{1,64}$/.test(name)} onClick={() => void run(async () => { await api.patch(path, { name }); setName(null); notify("名前を変更しました") })}>名前変更</button>
          </div>
        </div>
        {/* マウント先。 */}
        <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <h2 className="h2">マウント先 ({mounted.length})</h2>
          {mounted.length === 0 && <div className="mut">どのデプロイにもマウントされていません。</div>}
          {mounted.map((deployId) => <button key={deployId} className="opt" type="button" onClick={() => nav.openDeploy(deployId, "mounts")}><span className="mono">{deployId}</span></button>)}
        </div>
        <button className="btn danger" type="button" disabled={locked || busy || mounted.length > 0} onClick={() => setConfirming(true)}>{mounted.length > 0 ? "マウント中は削除できません" : "削除"}</button>
      </PanelFrame>
      {confirming && (
        <Modal title="ボリュームを削除しますか?" description={volume.name} onClose={() => setConfirming(false)} footer={<><button className="btn" type="button" onClick={() => setConfirming(false)}>キャンセル</button><button className="btn warn" type="button" disabled={busy} onClick={() => void run(async () => { await api.delete(path); setConfirming(false); notify("削除を開始しました"); onClose() })}>削除</button></>}>
          <div className="mut">データも削除されます。元に戻せません。</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => setToast(null)} />}
    </>
  )
}

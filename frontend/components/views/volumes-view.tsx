"use client"

import { useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import type { QuotaResponse, Volume, VolumeListResponse } from "@/lib/types"
import { ApplyBar, Badge, ErrorState, LoadingState, Modal, ToastNotice, isBusyStatus } from "@/components/ui-kit"

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
 * ボリュームの画面(モックの vVolumes と、右の詳細 drawer)。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.nav 画面を移る操作。
 * @param props.search 上のバーの検索の文字。
 * @param props.createTick 上のバーの「作成」が押された回数。
 */
export function VolumesView({ project, refreshInterval, nav, search, createTick }: ProjectViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: list, error, mutate } = useSWR<VolumeListResponse>(`${base}/volumes?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: quota } = useSWR<QuotaResponse>("/api/v1/quota", authenticatedFetcher)
  const maxMb = quota?.maxVolumeSizeMB ?? FALLBACK_MAX_MB
  const [openId, setOpenId] = useState("")
  const [creating, setCreating] = useState(false)
  const [toast, setToast] = useState("")
  const [toastError, setToastError] = useState(false)
  const lastTick = useRef(createTick)

  // 上のバーの「作成」が押されたら、作成のモーダルを開く。
  useEffect(() => {
    if (createTick === lastTick.current) return
    lastTick.current = createTick
    setCreating(true)
  }, [createTick])

  /**
   * 通知を出す。
   *
   * @param message 本文。
   * @param isError 失敗なら true。
   */
  function notify(message: string, isError = false) {
    setToast(message)
    setToastError(isError)
  }

  const needle = search.trim().toLowerCase()
  const volumes = (list?.volumes ?? []).filter((volume) => !needle || volume.name.toLowerCase().includes(needle) || volume.volumeId.toLowerCase().includes(needle))
  return (
    <>
      <div><h1 className="h1 disp">ボリューム</h1></div>
      {error && <ErrorState message="ボリューム一覧を取得できませんでした。" onRetry={() => void mutate()} />}
      {!list && !error && <LoadingState />}
      {list && volumes.length === 0 && <div className="mut">ボリュームがありません。</div>}
      {volumes.length > 0 && (
        <div className="card tscroll"><div className="tinner" style={{ minWidth: 820 }}>
          <div className="tr th" style={{ ["--cols" as string]: "1.4fr 1.2fr 110px 90px" }}><span>名前</span><span>容量</span><span>状態</span><span /></div>
          {volumes.map((volume) => (
            <div className="tr" key={volume.volumeId} style={{ ["--cols" as string]: "1.4fr 1.2fr 110px 90px", paddingTop: 14, paddingBottom: 14 }}>
              <div><button className="lnk" type="button" style={{ fontSize: 14 }} onClick={() => setOpenId(volume.volumeId)}>{volume.name} ›</button><div className="faint mono" style={{ fontSize: 11.5 }}>{volume.volumeId}</div></div>
              <div><span className="disp" style={{ fontSize: 18 }}>{sizeText(volume.sizeMB)}</span>{volume.pending_sizeMB !== null && <div className="row" style={{ gap: 6 }}><span className="mono" style={{ color: "var(--warn)" }}>→ {sizeText(volume.pending_sizeMB)}</span><span className="tag">保留中</span></div>}</div>
              <span><Badge status={volume.status} /></span>
              <div className="row" style={{ justifyContent: "flex-end" }}><button className="btn sm" type="button" onClick={() => setOpenId(volume.volumeId)}>詳細</button></div>
            </div>
          ))}
        </div></div>
      )}
      {openId && <VolumeDrawer base={base} volumeId={openId} maxMb={maxMb} refreshInterval={refreshInterval} nav={nav} notify={notify} onClose={() => setOpenId("")} onChanged={() => void mutate()} />}
      {creating && <CreateModal base={base} maxMb={maxMb} notify={notify} onClose={() => setCreating(false)} onCreated={() => void mutate()} />}
      {toast && <ToastNotice message={toast} type={toastError ? "error" : "success"} onDismiss={() => setToast("")} />}
    </>
  )
}

/**
 * ボリュームの作成のモーダル。
 *
 * @param props.base プロジェクトの API のパス。
 * @param props.maxMb 大きさの上限(MB)。
 * @param props.notify 通知を出す関数。
 * @param props.onClose 閉じるとき。
 * @param props.onCreated 作成できたとき。
 */
function CreateModal({ base, maxMb, notify, onClose, onCreated }: { base: string; maxMb: number; notify: (message: string, isError?: boolean) => void; onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState("")
  const [size, setSize] = useState("1024")
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState("")

  /** 検査して、作成を頼む。 */
  async function submit() {
    // 名前は英数字とハイフンで 1〜64 文字、大きさは 1〜上限の整数。
    const sizeMb = Number(size)
    if (!/^[A-Za-z0-9-]{1,64}$/.test(name.trim())) return setProblem("名前は、英数字とハイフンで 1〜64 文字にしてください。")
    if (!Number.isInteger(sizeMb) || sizeMb < 1 || sizeMb > maxMb) return setProblem(`大きさは、1〜${maxMb} MB の整数にしてください。`)
    setBusy(true)
    try {
      await api.post(`${base}/volumes`, { name: name.trim(), sizeMB: sizeMb })
      notify("ボリュームを作成しました")
      onCreated()
      onClose()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : "作成できませんでした")
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="ボリュームを作成" onClose={onClose} footer={<><button className="btn" type="button" onClick={onClose}>キャンセル</button><button className="btn primary" type="button" disabled={busy} onClick={() => void submit()}>作成</button></>}>
      <label className="lbl">名前<input className="inp" value={name} onChange={(event) => setName(event.target.value)} placeholder="db-data" autoFocus /></label>
      <label className="lbl">大きさ (MB)<input className="inp mono" inputMode="numeric" value={size} onChange={(event) => setSize(event.target.value)} /></label>
      <div className="faint" style={{ fontSize: 12.5 }}>最大 {maxMb} MB。作成後は、拡大だけできます。</div>
      {problem && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{problem}</div>}
    </Modal>
  )
}

/**
 * ボリュームの詳細(右の drawer)。名前の変更、拡大、反映待ち、マウント先、削除。
 *
 * @param props.base プロジェクトの API のパス。
 * @param props.volumeId ボリュームの ID。
 * @param props.maxMb 大きさの上限(MB)。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.nav 画面を移る操作。
 * @param props.notify 通知を出す関数。
 * @param props.onClose 閉じるとき。
 * @param props.onChanged 一覧が変わったとき。
 */
function VolumeDrawer({ base, volumeId, maxMb, refreshInterval, nav, notify, onClose, onChanged }: { base: string; volumeId: string; maxMb: number; refreshInterval: number; nav: ProjectViewProps["nav"]; notify: (message: string, isError?: boolean) => void; onClose: () => void; onChanged: () => void }) {
  const path = `${base}/volumes/${encodeURIComponent(volumeId)}`
  const { data: volume, error, mutate } = useSWR<Volume>(path, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [size, setSize] = useState("")
  const [name, setName] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState("")
  const [confirming, setConfirming] = useState(false)

  /**
   * 操作を実行し、終わったら取り直す(失敗は通知する)。
   *
   * @param action 実行する処理。
   */
  async function run(action: () => Promise<void>) {
    setBusy(true)
    try {
      await action()
      await mutate()
      onChanged()
    } catch (caught) {
      notify(caught instanceof Error ? caught.message : "操作に失敗しました", true)
    } finally {
      setBusy(false)
    }
  }

  const mounted = volume?.mountedBy ?? []
  const locked = volume ? isBusyStatus(volume.status) : true
  return (
    <>
      <button className="dback" type="button" onClick={onClose} aria-label="閉じる" />
      <aside className="drawer" role="dialog" aria-label="ボリューム詳細">
        {error && <ErrorState message="詳細を取得できませんでした。" onRetry={() => void mutate()} />}
        {!volume && !error && <LoadingState />}
        {volume && (
          <>
            <div className="row" style={{ alignItems: "flex-start", flexWrap: "nowrap" }}>
              <div className="grow"><h2 className="disp" style={{ margin: 0, fontSize: 20, fontWeight: 500 }}>{volume.name}</h2><div className="faint mono" style={{ marginTop: 2 }}>{volume.volumeId}</div></div>
              <Badge status={volume.status} />
              <button className="btn ico ghost" type="button" onClick={onClose} aria-label="閉じる">×</button>
            </div>
            {/* 大きさと、拡大。 */}
            <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12, background: "var(--bg)" }}>
              <div className="row" style={{ justifyContent: "space-between", alignItems: "baseline" }}><span className="mut">容量</span><span className="disp" style={{ fontSize: 22 }}>{sizeText(volume.sizeMB)}</span></div>
              {volume.pending_sizeMB !== null && <ApplyBar title={`拡張待ち: ${sizeText(volume.sizeMB)} → ${sizeText(volume.pending_sizeMB)}`} busy={busy || locked} onDiscard={() => void run(async () => { await api.post(`${path}/discard`); notify("破棄しました") })} onApply={() => void run(async () => { await api.post(`${path}/apply`); notify("拡張を開始しました") })} />}
              <div className="row" style={{ gap: 6, flexWrap: "nowrap" }}>
                <input className="inp mono" style={{ width: 120, minHeight: 36 }} type="number" value={size} onChange={(event) => { setSize(event.target.value); setProblem("") }} disabled={locked} placeholder={String(volume.sizeMB)} aria-label="新しい容量 (MB)" />
                <span className="faint mono">MB</span>
                <button className="btn sm" type="button" disabled={locked || busy || !size} onClick={() => {
                  // 拡大だけ許す(縮小はできない)。上限も守る。
                  const next = Number(size)
                  if (!Number.isInteger(next) || next <= volume.sizeMB) return setProblem(`現在(${volume.sizeMB} MB)より大きい整数にしてください。縮小はできません。`)
                  if (next > maxMb) return setProblem(`上限は ${maxMb} MB です。`)
                  void run(async () => { await api.patch(`${path}/size`, { sizeMB: next }); setSize(""); notify("拡張を保存しました(未適用)") })
                }}>拡張を保存</button>
              </div>
              {problem && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{problem}</div>}
            </div>
            {/* 名前の変更。 */}
            <div className="row" style={{ flexWrap: "nowrap", gap: 8 }}>
              <input className="inp" value={name ?? volume.name} onChange={(event) => setName(event.target.value)} disabled={locked} aria-label="名前" />
              <button className="btn sm" type="button" disabled={locked || busy || name === null || name === volume.name || !/^[A-Za-z0-9-]{1,64}$/.test(name)} onClick={() => void run(async () => { await api.patch(path, { name }); setName(null); notify("名前を変更しました") })}>名前変更</button>
            </div>
            {/* マウント先。 */}
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              <h3 className="h2">マウント先 ({mounted.length})</h3>
              {mounted.length === 0 && <div className="mut">どのデプロイにもマウントされていません。</div>}
              {mounted.map((deployId) => <button key={deployId} className="opt" type="button" style={{ minHeight: 56 }} onClick={() => nav.openDeploy(deployId, "mounts")}><span className="mono">{deployId}</span></button>)}
            </div>
            <button className="btn danger" type="button" style={{ marginTop: "auto" }} disabled={locked || busy || mounted.length > 0} onClick={() => setConfirming(true)}>{mounted.length > 0 ? "マウント中は削除できません" : "削除"}</button>
            {confirming && (
              <Modal title="ボリュームを削除しますか?" description={volume.name} onClose={() => setConfirming(false)} footer={<><button className="btn" type="button" onClick={() => setConfirming(false)}>キャンセル</button><button className="btn warn" type="button" disabled={busy} onClick={() => void run(async () => { await api.delete(path); setConfirming(false); notify("削除を開始しました"); onClose() })}>削除</button></>}>
                <div className="mut">データも削除されます。元に戻せません。</div>
              </Modal>
            )}
          </>
        )}
      </aside>
    </>
  )
}

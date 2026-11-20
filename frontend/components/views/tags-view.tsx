"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import type { DeployListResponse, IngressListResponse, TagDeleteResponse, TagListResponse, VolumeListResponse } from "@/lib/types"
import { revalidateProject } from "@/lib/revalidate"
import { EmptyState, ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"
import { MetricsPanel } from "@/components/metrics-panel"

/**
 * プロジェクトの「タグ」の画面。タグの一覧(deploy の数つき)と、選んだタグの deploy、メトリクスを見せ、
 * タグの付いた deploy を、まとめて削除できる。タグは、deploy の「概要」タブで付ける(テンプレートからは、自動で付く)。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。0 なら止める)。
 * @param props.nav 画面を移る操作(deploy を開く)。
 */
export function TagsView({ project, refreshInterval, nav }: ProjectViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const [selected, setSelected] = useState("")
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)
  const { data, error, mutate } = useSWR<TagListResponse>(`${base}/tags`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true, refreshWhenHidden: false })
  const { data: deployList } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, { refreshInterval, refreshWhenHidden: false })
  const { data: volumeList } = useSWR<VolumeListResponse>(`${base}/volumes?limit=100`, authenticatedFetcher, { refreshInterval, refreshWhenHidden: false })
  const { data: ingressList } = useSWR<IngressListResponse>(`${base}/ingresses?limit=100`, authenticatedFetcher, { refreshInterval, refreshWhenHidden: false })

  if (error && !data) return <ErrorState message="タグを取得できませんでした" onRetry={() => void mutate()} />
  if (!data) return <LoadingState />
  // 選んだタグが、無くなったら(全て削除した、など)、選択を外す。
  const tags = data.tags
  const current = tags.find((item) => item.tag === selected)
  const has = (tags?: string[]) => Boolean(current) && (tags ?? []).includes(current!.tag)
  // タグの付いた資源(service は、deploy のパネルの Service タブにあるので、数だけ)。
  const deployMembers = (deployList?.deploys ?? []).filter((item) => has(item.tags))
  const volumeMembers = (volumeList?.volumes ?? []).filter((item) => has(item.tags))
  const ingressMembers = ((ingressList?.ingresses ?? []) as { ingressId: string; host: string; tags?: string[] }[]).filter((item) => has(item.tags))
  const total = current ? current.deployCount + current.serviceCount + current.ingressCount : 0

  /** 選んだタグの deploy を、一斉に削除する(受け付けた数と、失敗の数を、知らせる)。 */
  async function deleteAll() {
    if (!current) return
    setBusy(true)
    try {
      const result = await api.delete<TagDeleteResponse>(`${base}/tags/${encodeURIComponent(current.tag)}/deploys`)
      setConfirmDelete(false)
      setToast(result.failed.length === 0 ? { message: `${result.accepted.length} 個の削除を始めました`, type: "success" } : { message: `${result.accepted.length} 個の削除を始め、${result.failed.length} 個は受け付けられませんでした(${result.failed[0].kind}: ${result.failed[0].message})`, type: "error" })
      await mutate()
      await revalidateProject(project.projectId)
    } catch (deleteError) {
      setToast({ message: deleteError instanceof Error ? deleteError.message : "削除できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="col" style={{ gap: 14 }}>
      <h1 className="h1">タグ</h1>
      {tags.length === 0 && <EmptyState title="タグがありません" description="deploy の「概要」タブで、タグを付けられます。テンプレートから作った deploy には、template:<名前> と apply:<識別> が自動で付きます。" />}
      <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
        {tags.map((item) => <button key={item.tag} type="button" className={`btn sm mono ${selected === item.tag ? "primary" : ""}`} onClick={() => setSelected(item.tag)}>{item.tag}({item.deployCount + item.volumeCount + item.serviceCount + item.ingressCount})</button>)}
      </div>
      {current && (
        <>
          <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div className="row">
              <h2 className="h2 grow mono">{current.tag}</h2>
              <button className="btn danger" type="button" onClick={() => { setConfirmDelete(true) }} disabled={busy}>このタグのものを、まとめて削除</button>
            </div>
            <div className="faint" style={{ fontSize: 12.5 }}>deploy {current.deployCount} ・ service {current.serviceCount} ・ volume {current.volumeCount} ・ ingress {current.ingressCount}</div>
            {deployMembers.map((item) => <button key={item.deployId} type="button" className="opt" onClick={() => nav.openDeploy(item.deployId)}><span className="faint">deploy</span><span className="mono">{item.deployId}</span><span className="grow" /><span className="faint">{item.status} / {item.spec}</span></button>)}
            {volumeMembers.map((item) => <button key={item.volumeId} type="button" className="opt" onClick={() => nav.select({ kind: "volume", id: item.volumeId })}><span className="faint">volume</span><span className="mono">{item.name}</span><span className="grow" /><span className="faint">{item.status}</span></button>)}
            {ingressMembers.map((item) => <button key={item.ingressId} type="button" className="opt" onClick={() => nav.select({ kind: "ingress", id: item.ingressId })}><span className="faint">ingress</span><span className="mono">{item.host}</span></button>)}
          </div>
          <MetricsPanel key={current.tag} path={`${base}/tags/${encodeURIComponent(current.tag)}`} refreshInterval={refreshInterval} />
        </>
      )}
      {confirmDelete && current && (
        <Modal title={`タグ「${current.tag}」のものを、削除しますか?`} description={`${total} 個(deploy ${current.deployCount}、service ${current.serviceCount}、ingress ${current.ingressCount})を削除します。volume と ConfigMap は、データを守るため、残します(個別に削除してください)。元に戻せません。反映中・起動中のものは、受け付けられません。`} onClose={() => setConfirmDelete(false)}
          footer={<><button className="btn" type="button" onClick={() => setConfirmDelete(false)}>キャンセル</button><button className="btn danger" type="button" onClick={() => void deleteAll()} disabled={busy}>まとめて削除</button></>}>
          <div className="mono">{[...deployMembers.map((item) => item.deployId), ...volumeMembers.map((item) => item.name), ...ingressMembers.map((item) => item.host)].join("\n")}</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div>
  )
}

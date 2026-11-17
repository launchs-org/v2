"use client"

import { useEffect, useMemo, useState } from "react"
import useSWR from "swr"
import { ExternalLink, Globe2, Plus, RefreshCw, Route, Trash2 } from "lucide-react"
import { api, authenticatedFetcher } from "@/lib/api"
import type { DeployListResponse, IngressDetail, IngressListResponse, IngressRule, Project, ServiceInfo, ServicePort } from "@/lib/types"
import { EmptyState, ErrorState, LoadingState, Modal, PageHeading, StatusPill, ToastNotice } from "@/components/ui-kit"

type ServiceOption = { deployLabel: string; serviceId: string; ports: ServicePort[] }

export function IngressesView({ project, refreshInterval }: { project: Project; refreshInterval: number }) {
  const projectPath = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: ingressData, error, mutate: refreshList } = useSWR<IngressListResponse>(`${projectPath}/ingresses?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: deployData } = useSWR<DeployListResponse>(`${projectPath}/deploys?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [selectedId, setSelectedId] = useState("")
  const [rules, setRules] = useState<IngressRule[]>([])
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [modal, setModal] = useState<"create" | "delete" | null>(null)
  const [toast, setToast] = useState("")
  const [toastError, setToastError] = useState(false)
  const ingresses = ingressData?.ingresses ?? []
  const activeId = ingresses.some((ingress) => ingress.ingressId === selectedId) ? selectedId : ingresses[0]?.ingressId ?? ""
  const { data: detail, error: detailError, mutate: refreshDetail } = useSWR<IngressDetail>(activeId ? `${projectPath}/ingresses/${encodeURIComponent(activeId)}` : null, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })

  const deploys = deployData?.deploys ?? []
  const serviceKey = detail && deploys.length ? `services:${project.projectId}:${deploys.map((deploy) => deploy.deployId).join(",")}` : null
  const fetchServices = async () => {
    const results = await Promise.all(deploys.map(async (deploy) => {
      try {
        const service = await api.get<ServiceInfo>(`/api/v1/deploy/${encodeURIComponent(deploy.deployId)}/service`)
        return service?.status === "active" || service?.status === "pending" ? { deployLabel: deploy.imageRef.split("/").pop() ?? deploy.deployId, serviceId: service.serviceId, ports: service.pending_ports ?? service.ports } : null
      } catch { return null }
    }))
    return results.filter((service): service is ServiceOption => Boolean(service))
  }
  const { data: services } = useSWR<ServiceOption[]>(serviceKey, fetchServices, { refreshInterval: 30_000, revalidateOnFocus: true })
  const selectedServiceMap = useMemo(() => new Map((services ?? []).map((service) => [service.serviceId, service])), [services])
  const pending = detail?.pending_rules !== null && detail !== undefined

  useEffect(() => {
    if (detail && !dirty) setRules(detail.pending_rules ?? detail.rules ?? [])
  }, [detail, dirty])

  function notify(message: string, isError = false) { setToast(message); setToastError(isError) }

  async function createIngress() {
    setBusy(true)
    try {
      const result = await api.post<{ ingressId: string; host: string }>(`${projectPath}/ingresses`)
      await refreshList()
      setSelectedId(result.ingressId)
      setModal(null)
      notify("Ingress を作成しました。ルールを設定してから Apply してください")
    } catch (caught) { notify(caught instanceof Error ? caught.message : "Ingress を作成できませんでした", true) }
    finally { setBusy(false) }
  }

  async function saveRules() {
    if (rules.some((rule) => !rule.path.startsWith("/") || !rule.serviceId || rule.port < 1 || rule.port > 65535)) {
      notify("パスは / から始め、Service と有効なポートを指定してください", true)
      return
    }
    setBusy(true)
    try {
      await api.put(`${projectPath}/ingresses/${encodeURIComponent(activeId)}`, { rules })
      setDirty(false)
      notify("Ingress ルールを pending として保存しました")
      void refreshDetail()
    } catch (caught) { notify(caught instanceof Error ? caught.message : "ルールを保存できませんでした", true) }
    finally { setBusy(false) }
  }

  async function pendingAction(action: "apply" | "discard") {
    setBusy(true)
    try {
      await api.post(`${projectPath}/ingresses/${encodeURIComponent(activeId)}/${action}`)
      setDirty(false)
      notify(action === "apply" ? "Ingress ルールの反映を開始しました。状態は自動更新されます" : "Ingress の下書きを破棄しました")
      void refreshDetail()
    } catch (caught) { notify(caught instanceof Error ? caught.message : "操作を完了できませんでした", true) }
    finally { setBusy(false) }
  }

  async function deleteIngress() {
    setBusy(true)
    try {
      await api.delete(`${projectPath}/ingresses/${encodeURIComponent(activeId)}`)
      setModal(null)
      setSelectedId("")
      notify("Ingress の削除を開始しました")
      void refreshList()
    } catch (caught) { notify(caught instanceof Error ? caught.message : "Ingress を削除できませんでした", true) }
    finally { setBusy(false) }
  }

  function updateRule(index: number, updates: Partial<IngressRule>) {
    setRules((current) => current.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, ...updates } : rule))
    setDirty(true)
  }

  return <section>
    <PageHeading eyebrow="EDGE ROUTING" title="Ingress 管理" description="外部公開ホストとパスルーティングを設定し、Service へトラフィックを転送します。" actions={<button className="btn btn-primary" type="button" onClick={() => setModal("create")}><Plus size={15} />Ingress を作成</button>} />
    {error && <ErrorState message="Ingress 一覧を取得できませんでした。" onRetry={() => void refreshList()} />}
    <div className="ingress-layout">
      <aside className="card ingress-list-card"><div className="list-heading"><div><strong>公開ホスト</strong><span>{ingressData ? `${ingresses.length} 件` : "読み込み中"}</span></div><button className="icon-button" type="button" onClick={() => void refreshList()} title="更新" aria-label="Ingress を更新"><RefreshCw size={14} /></button></div>{!ingressData ? <div className="list-loading"><span className="loader" />取得中</div> : !ingresses.length ? <EmptyState icon={<Globe2 size={16} />} title="Ingress なし" description="ホストを作成して、Service へのルートを追加します。" action={<button className="btn btn-secondary btn-sm" type="button" onClick={() => setModal("create")}><Plus size={13} />作成</button>} /> : <div className="ingress-list">{ingresses.map((ingress) => <button key={ingress.ingressId} type="button" className={`ingress-list-item ${activeId === ingress.ingressId ? "selected" : ""}`} onClick={() => { setSelectedId(ingress.ingressId); setDirty(false) }}><span className="ingress-list-icon"><Globe2 size={15} /></span><span><strong>{ingress.host}</strong><small>{ingress.ingressId.slice(0, 20)}</small></span></button>)}</div>}</aside>
      <div className="card ingress-detail-card">{activeId && !detail && !detailError && <LoadingState label="Ingress 詳細を取得しています" />}{detailError && <ErrorState message="Ingress 詳細を取得できませんでした。" onRetry={() => void refreshDetail()} />}{detail && <>
        <div className="ingress-detail-header"><div className="ingress-host-block"><span className="host-icon"><Route size={16} /></span><div><span className="small-label">PUBLIC HOST</span><h2>{detail.host}</h2><a href={`https://${detail.host}`} target="_blank" rel="noreferrer" className="host-link">https://{detail.host}<ExternalLink size={12} /></a></div></div><div className="ingress-header-actions"><StatusPill status={detail.status} /><button className="icon-button danger-icon" type="button" onClick={() => setModal("delete")} disabled={detail.status === "deleting" || detail.status === "applying"} title="Ingress を削除" aria-label="Ingress を削除"><Trash2 size={14} /></button></div></div>
        <div className="route-section-heading"><div><h3>ルーティングルール</h3><p>パスごとに転送先の Service とポートを指定します。</p></div>{pending && <span className="draft-label">DRAFT PENDING</span>}</div>
        {!services?.length && <div className="info-banner"><Globe2 size={15} />{deploys.length ? "Service 情報を取得中、または公開可能な Service がありません。先にデプロイ管理で Service を作成してください。" : "ルーティング先のデプロイがありません。先にデプロイを作成してください。"}</div>}
        <div className="ingress-rules-header"><span>PATH</span><span>SERVICE</span><span>PORT</span><span className="sr-only">削除</span></div>
        {rules.map((rule, index) => { const service = selectedServiceMap.get(rule.serviceId); return <div className="ingress-rule-row" key={`${index}-${rule.path}`}><input className="form-control mono-input" aria-label={`ルール ${index + 1} のパス`} value={rule.path} onChange={(event) => updateRule(index, { path: event.target.value })} placeholder="/api" /><select className="select-control" aria-label={`ルール ${index + 1} の Service`} value={rule.serviceId} onChange={(event) => { const next = selectedServiceMap.get(event.target.value); updateRule(index, { serviceId: event.target.value, port: next?.ports[0]?.port ?? rule.port }) }}><option value="">Service を選択</option>{(services ?? []).map((option) => <option key={option.serviceId} value={option.serviceId}>{option.deployLabel} · {option.serviceId.slice(0, 12)}</option>)}</select><select className="select-control" aria-label={`ルール ${index + 1} のポート`} value={rule.port} onChange={(event) => updateRule(index, { port: Number(event.target.value) })}>{(service?.ports ?? [{ name: String(rule.port), port: rule.port, protocol: "tcp" as const }]).map((port) => <option key={`${port.name}-${port.port}`} value={port.port}>{port.name} · {port.port}/{port.protocol.toUpperCase()}</option>)}</select><button className="icon-button danger-icon" type="button" onClick={() => { setRules((current) => current.filter((_, ruleIndex) => ruleIndex !== index)); setDirty(true) }} aria-label={`ルール ${index + 1} を削除`}><Trash2 size={14} /></button></div> })}
        <div className="route-edit-actions"><button className="btn btn-ghost btn-sm" type="button" onClick={() => { setRules((current) => [...current, { path: "/", serviceId: services?.[0]?.serviceId ?? "", port: services?.[0]?.ports[0]?.port ?? 8080 }]); setDirty(true) }}><Plus size={14} />ルールを追加</button><button className="btn btn-secondary" type="button" onClick={() => void saveRules()} disabled={busy || !dirty}>{busy ? "保存中…" : "下書きを保存"}</button></div>
        {pending && <div className="pending-bar"><div className="pending-copy"><span className="pending-dot" /><div><strong>未反映のルールがあります</strong><span>Apply で外部公開設定へ反映します</span></div></div><div className="pending-actions"><button className="btn btn-secondary btn-sm" type="button" onClick={() => void pendingAction("discard")} disabled={busy}>破棄</button><button className="btn btn-primary btn-sm" type="button" onClick={() => void pendingAction("apply")} disabled={busy || detail.status === "applying" || detail.status === "deleting"}><RefreshCw size={13} />Apply</button></div></div>}
        <div className="info-banner ingress-note"><Route size={15} /><span>変更は pending として保存され、Apply までは現在のルーティングを維持します。</span></div>
      </>}</div>
    </div>
    {modal === "create" && <Modal title="Ingress を作成" description="ホスト名はシステムが自動で割り当てます。作成後にルーティングルールを設定できます。" onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-primary" type="button" onClick={() => void createIngress()} disabled={busy}>{busy ? "作成中…" : <><Plus size={14} />Ingress を作成</>}</button></>}><div className="info-banner"><Globe2 size={15} />作成されたホストは公開 URL になります。ルールの設定後に Apply してください。</div></Modal>}
    {modal === "delete" && <Modal title="Ingress を削除しますか？" description="ホストとルーティング設定を削除します。" onClose={() => setModal(null)} footer={<><button className="btn btn-secondary" type="button" onClick={() => setModal(null)}>キャンセル</button><button className="btn btn-danger" type="button" onClick={() => void deleteIngress()} disabled={busy}>{busy ? "削除中…" : "削除する"}</button></>}><div className="delete-warning"><Globe2 size={16} /><strong>{detail?.host}</strong></div></Modal>}
    {toast && <ToastNotice message={toast} type={toastError ? "error" : "success"} onDismiss={() => setToast("")} />}
  </section>
}

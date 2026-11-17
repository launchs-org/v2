"use client"

import useSWR from "swr"
import { Activity, ArrowDownRight, ArrowRight, ArrowUpRight, Box, CircleHelp, Database, Globe2, Layers3, Plus } from "lucide-react"
import { authenticatedFetcher } from "@/lib/api"
import type { BuildListResponse, DeployListResponse, Project, QuotaResponse, VolumeListResponse, IngressListResponse } from "@/lib/types"
import { EmptyState, ErrorState, formattedDate, PageHeading, StatusPill } from "@/components/ui-kit"

type OverviewViewProps = {
  project: Project
  refreshInterval: number
  onNavigate: (section: "deployments" | "volumes" | "ingresses" | "projects") => void
}

function UsageMeter({ label, usage }: { label: string; usage?: { used: number; limit: number } }) {
  const percentage = usage && usage.limit > 0 ? Math.min((usage.used / usage.limit) * 100, 100) : 0
  return <div className="usage-meter"><div className="usage-label"><span>{label}</span><strong>{usage ? <>{usage.used} <span>/ {usage.limit}</span></> : "—"}</strong></div><div className="meter-track"><span style={{ width: `${percentage}%` }} /></div></div>
}

export function OverviewView({ project, refreshInterval, onNavigate }: OverviewViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: deployData, error: deployError, mutate: refreshDeploys } = useSWR<DeployListResponse>(`${base}/deploys?limit=50`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: volumeData, error: volumeError } = useSWR<VolumeListResponse>(`${base}/volumes?limit=50`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: ingressData, error: ingressError } = useSWR<IngressListResponse>(`${base}/ingresses?limit=50`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: quota } = useSWR<QuotaResponse>("/api/v1/quota", authenticatedFetcher, { refreshInterval: refreshInterval ? 15_000 : 0, revalidateOnFocus: true })
  const { data: builds } = useSWR<BuildListResponse>(`${base}/builds?limit=5`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const deploys = deployData?.deploys ?? []
  const activeDeploys = deploys.filter((deploy) => deploy.status === "active")
  const healthyReplicas = activeDeploys.reduce((sum, deploy) => sum + deploy.replicas, 0)
  const waiting = deploys.filter((deploy) => ["creating", "applying", "scaling", "pending"].includes(deploy.status)).length
  const latestDeploy = deploys[0]

  return (
    <section>
      <PageHeading eyebrow="PROJECT OVERVIEW" title="プロジェクト概要" description={`${project.name} のリソースと稼働状況をリアルタイムで確認できます。`} actions={<button className="btn btn-primary" type="button" onClick={() => onNavigate("deployments")}><Plus size={15} />デプロイを作成</button>} />
      {(deployError || volumeError || ingressError) && <ErrorState message="一部のリソースを読み込めませんでした。自動更新時に再試行します。" onRetry={() => void refreshDeploys()} />}

      <div className="overview-hero">
        <div className="hero-copy"><div className="hero-eyebrow"><span className="live-dot" />LIVE PROJECT</div><div className="hero-title-row"><h2>{project.name}</h2><StatusPill status={project.status} /></div><p>クラスターのリソース状態を監視しています。変更は下書き保存後に apply で反映されます。</p><div className="hero-stat-line"><span><strong>{healthyReplicas}</strong> healthy replicas</span><span className="hero-divider" /><span><strong>{waiting}</strong> changes in progress</span></div></div>
        <div className="hero-orbit" aria-hidden="true"><div className="orbit-ring orbit-ring-one" /><div className="orbit-ring orbit-ring-two" /><div className="orbit-core"><Box size={25} /></div><span className="orbit-node orbit-node-one" /><span className="orbit-node orbit-node-two" /><span className="orbit-node orbit-node-three" /></div>
        <div className="hero-graph" aria-hidden="true"><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /><span /></div>
      </div>

      <div className="metric-grid">
        <article className="metric-card"><div className="metric-top"><span className="metric-icon blue"><Box size={16} /></span><span className="metric-caption">DEPLOYMENTS</span><ArrowUpRight size={15} className="metric-arrow" /></div><div className="metric-value">{deployData ? deploys.length : "—"}</div><div className="metric-foot"><span className="metric-positive"><span className="live-dot" />{activeDeploys.length} 稼働中</span><span>このプロジェクト</span></div></article>
        <article className="metric-card"><div className="metric-top"><span className="metric-icon violet"><Layers3 size={16} /></span><span className="metric-caption">READY REPLICAS</span><ArrowUpRight size={15} className="metric-arrow" /></div><div className="metric-value">{deployData ? healthyReplicas : "—"}<span className="metric-suffix"> pods</span></div><div className="metric-foot"><span>{waiting ? `${waiting} 件の変更を反映中` : "すべて最新です"}</span><span>max 5 / deploy</span></div></article>
        <article className="metric-card"><div className="metric-top"><span className="metric-icon teal"><Database size={16} /></span><span className="metric-caption">VOLUMES</span><ArrowUpRight size={15} className="metric-arrow" /></div><div className="metric-value">{volumeData ? volumeData.volumes.length : "—"}</div><div className="metric-foot"><span>{volumeData?.volumes.reduce((sum, volume) => sum + volume.sizeMB, 0).toLocaleString() ?? "—"} MB allocated</span><span>{volumeData?.volumes.filter((volume) => volume.pending_sizeMB !== null).length ?? 0} pending</span></div></article>
        <article className="metric-card"><div className="metric-top"><span className="metric-icon orange"><Globe2 size={16} /></span><span className="metric-caption">INGRESS RULES</span><ArrowUpRight size={15} className="metric-arrow" /></div><div className="metric-value">{ingressData ? ingressData.ingresses.length : "—"}</div><div className="metric-foot"><span>外部公開ルート</span><span>プロジェクト内</span></div></article>
      </div>

      <div className="overview-columns">
        <div className="card usage-card">
          <div className="card-heading"><div><h2>リソース使用量</h2><p>アカウントのクォータと現在値</p></div><span className="heading-icon"><Activity size={15} /></span></div>
          {quota ? <div className="usage-list"><UsageMeter label="デプロイ" usage={quota.deploys} /><UsageMeter label="ボリューム" usage={quota.volumes} /><UsageMeter label="ストレージ容量" usage={quota.volumeTotalMB} /></div> : <div className="usage-unavailable"><CircleHelp size={15} />クォータ情報を取得しています</div>}
          <div className="quota-foot"><span>レプリカ上限 <strong>{quota?.maxReplicasPerDeploy ?? "—"}</strong> / deploy</span><span>ボリューム上限 <strong>{quota?.maxVolumeSizeMB.toLocaleString() ?? "—"} MB</strong></span></div>
        </div>
        <div className="card recent-card">
          <div className="card-heading"><div><h2>最近のデプロイ</h2><p>直近のデプロイ状態</p></div><button className="text-link" type="button" onClick={() => onNavigate("deployments")}>すべて表示<ArrowRight size={13} /></button></div>
          {!deployData ? <div className="skeleton-row"><span /><span /><span /></div> : deploys.length === 0 ? <EmptyState icon={<Box size={17} />} title="デプロイはまだありません" description="イメージを指定して最初のデプロイを作成できます。" action={<button className="btn btn-secondary btn-sm" type="button" onClick={() => onNavigate("deployments")}><Plus size={14} />デプロイを作成</button>} /> : <div className="recent-list">{deploys.slice(0, 4).map((deploy) => <button className="recent-item" key={deploy.deployId} type="button" onClick={() => onNavigate("deployments")}><span className="recent-status-icon"><Box size={15} /></span><span className="recent-copy"><strong>{deploy.imageRef.split("/").pop()}</strong><small>{deploy.deployId.slice(0, 22)} · {formattedDate(deploy.createdAt)}</small></span><StatusPill status={deploy.status} /></button>)}</div>}
        </div>
      </div>

      <div className="card build-strip">
        <div className="build-strip-icon"><ArrowDownRight size={16} /></div><div className="build-strip-copy"><strong>ビルド履歴</strong><span>{builds?.builds.length ? `${builds.builds.length} 件の最近のビルド` : "ビルド履歴を確認"}</span></div><div className="build-strip-items">{builds?.builds.slice(0, 3).map((build) => <span className="build-chip" key={build.buildId}><StatusPill status={build.status} /><code>{build.buildId.slice(0, 15)}</code></span>)}</div><button className="icon-button" type="button" onClick={() => onNavigate("deployments")} aria-label="デプロイ管理へ"><ArrowRight size={15} /></button>
      </div>
    </section>
  )
}

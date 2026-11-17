"use client"

import { useState } from "react"
import useSWR from "swr"
import { Activity, FileCode2, RefreshCw } from "lucide-react"
import { authenticatedFetcher } from "@/lib/api"
import type { DeployDetail, DeployListResponse, Project } from "@/lib/types"
import { ErrorState, LoadingState, PageHeading, StatusPill } from "@/components/ui-kit"
import { LogTail } from "@/components/log-tail"

export function LogsView({ project, refreshInterval }: { project: Project; refreshInterval: number }) {
  const projectPath = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: list, error, mutate: refreshList } = useSWR<DeployListResponse>(`${projectPath}/deploys?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [selectedId, setSelectedId] = useState("")
  const deploys = list?.deploys ?? []
  const activeId = deploys.some((deploy) => deploy.deployId === selectedId) ? selectedId : deploys[0]?.deployId ?? ""
  const { data: detail, error: detailError, mutate: refreshDetail } = useSWR<DeployDetail>(activeId ? `/api/v1/deploy/${encodeURIComponent(activeId)}` : null, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })

  return <section>
    <PageHeading eyebrow="OBSERVABILITY" title="ログ管理" description="Pod ごとのコンテナログを追跡します。ログは差分ポーリングで自動取得します。" actions={<button className="btn btn-outline" type="button" onClick={() => { void refreshList(); void refreshDetail() }}><RefreshCw size={14} />今すぐ更新</button>} />
    {error && <ErrorState message="デプロイ一覧を取得できませんでした。" onRetry={() => void refreshList()} />}
    {!list ? <div className="card"><LoadingState label="デプロイ一覧を読み込んでいます" /></div> : !deploys.length ? <div className="card"><div className="empty-state"><div className="empty-icon"><FileCode2 size={18} /></div><strong>ログを表示できるデプロイがありません</strong><p>デプロイを作成すると、Pod のログをここで確認できます。</p></div></div> : <div className="card logs-page-card">
      <div className="logs-page-header"><div className="logs-deploy-select"><label className="form-label" htmlFor="logs-deploy-select">デプロイ</label><select id="logs-deploy-select" className="select-control" value={activeId} onChange={(event) => setSelectedId(event.target.value)}>{deploys.map((deploy) => <option key={deploy.deployId} value={deploy.deployId}>{deploy.imageRef.split("/").pop()} · {deploy.deployId.slice(0, 12)}</option>)}</select></div><div className="logs-deploy-status">{detail && <StatusPill status={detail.status} />}<span className="logs-polling-note"><span className="live-dot" />{refreshInterval ? "自動更新中" : "更新停止中"}</span></div></div>
      {detailError && <ErrorState message="デプロイ詳細を取得できませんでした。" onRetry={() => void refreshDetail()} />}
      {!detail && !detailError ? <div className="detail-content"><LoadingState label="Pod 一覧を取得しています" /></div> : detail && <div className="log-page-inner"><div className="logs-target-row"><div><div className="small-label">LOG SOURCE</div><strong>{detail.logPods.length} Pods</strong></div><span className="logs-target-divider" /><div><div className="small-label">IMAGE</div><code className="code-text">{detail.imageRef}</code></div><span className="logs-target-spacer" /><span className="small-muted"><Activity size={13} /> 最新の出力が下に追加されます</span></div><LogTail key={`${activeId}:${detail.logPods.map((pod) => pod.podId).join(",")}`} deployId={activeId} pods={detail.logPods} refreshInterval={refreshInterval} /></div>}
    </div>}
  </section>
}

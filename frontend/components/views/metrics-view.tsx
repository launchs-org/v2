"use client"

import { useState } from "react"
import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import type { DeployListResponse, TagListResponse } from "@/lib/types"
import { MetricsPanel } from "@/components/metrics-panel"

/**
 * プロジェクトの「メトリクス」の画面。見る範囲を、プロジェクト全体、タグ、deploy 1 つから選んで、CPU・メモリなどを見る。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。0 なら止める)。
 */
export function MetricsView({ project, refreshInterval }: Pick<ProjectViewProps, "project" | "refreshInterval">) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  // 見る範囲: "project"(全体)、"tag:<タグ>"、"deploy:<ID>"。
  const [scope, setScope] = useState("project")
  const { data: tagList } = useSWR<TagListResponse>(`${base}/tags`, authenticatedFetcher, { refreshInterval: 0 })
  const { data: deployList } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, { refreshInterval: 0 })
  // 範囲に合わせて、メトリクスを取る API のパスを決める。
  const path = scope.startsWith("tag:") ? `${base}/tags/${encodeURIComponent(scope.slice(4))}` : scope.startsWith("deploy:") ? `/api/v1/deploy/${encodeURIComponent(scope.slice(7))}` : base
  return (
    <div className="col" style={{ gap: 14 }}>
      <div className="row" style={{ gap: 10 }}>
        <h1 className="h1 grow">メトリクス</h1>
        <select className="inp" value={scope} onChange={(event) => setScope(event.target.value)} aria-label="見る範囲">
          <option value="project">プロジェクト全体</option>
          <optgroup label="タグ">{(tagList?.tags ?? []).map((item) => <option key={item.tag} value={`tag:${item.tag}`}>{item.tag}({item.deployCount})</option>)}</optgroup>
          <optgroup label="deploy">{(deployList?.deploys ?? []).map((item) => <option key={item.deployId} value={`deploy:${item.deployId}`}>{item.deployId}</option>)}</optgroup>
        </select>
      </div>
      <MetricsPanel key={scope} path={path} refreshInterval={refreshInterval} />
    </div>
  )
}

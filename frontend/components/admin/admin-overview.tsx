"use client"

import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import { ErrorState, LoadingState } from "@/components/ui-kit"

type StatusCount = { status: string; count: number }
type Overview = { projects: StatusCount[]; deploys: StatusCount[]; ingresses: StatusCount[]; volumes: StatusCount[]; owners: number }

/** 件数の合計。 */
function total(counts: StatusCount[]) {
  return counts.reduce((sum, item) => sum + item.count, 0)
}

/** 1 種類の資源の、合計と状態ごとの数のカード。 */
function CountCard({ title, counts }: { title: string; counts: StatusCount[] }) {
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div className="faint" style={{ fontSize: 12.5 }}>{title}</div>
      <div className="disp" style={{ fontSize: 30 }}>{total(counts)}</div>
      <div className="faint" style={{ fontSize: 12.5 }}>{counts.length === 0 ? "なし" : counts.map((item) => `${item.status} ${item.count}`).join(" ・ ")}</div>
    </div>
  )
}

/**
 * 全体の概要(管理コンテナの /admin-api/overview)。プロジェクト、deploy、ingress、volume の数と、状態ごとの内訳。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function AdminOverview({ refreshInterval }: { refreshInterval: number }) {
  const { data, error, mutate } = useSWR<Overview>("/admin-api/overview", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  if (!data && !error) return <LoadingState />
  if (!data) return <ErrorState message="概要を取得できませんでした" onRetry={() => void mutate()} />
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(min(220px, 100%), 1fr))", gap: 14 }}>
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <div className="faint" style={{ fontSize: 12.5 }}>利用者(プロジェクトを持つ人)</div>
        <div className="disp" style={{ fontSize: 30 }}>{data.owners}</div>
      </div>
      <CountCard title="プロジェクト" counts={data.projects} />
      <CountCard title="deploy" counts={data.deploys} />
      <CountCard title="ingress" counts={data.ingresses} />
      <CountCard title="volume" counts={data.volumes} />
    </div>
  )
}

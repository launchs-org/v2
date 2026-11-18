"use client"

import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import type { QuotaResponse, QuotaUsage } from "@/lib/types"
import { ErrorState, LoadingState } from "@/components/ui-kit"

/**
 * 使用量のバー1本(8割を超えたら、橙にする)。
 *
 * @param props.label 項目名。
 * @param props.usage 使用量と上限。
 * @param props.unit 単位(例: " MB")。
 */
function Meter({ label, usage, unit = "" }: { label: string; usage: QuotaUsage; unit?: string }) {
  // 上限が 0 のときは、割り算しない。
  const ratio = usage.limit > 0 ? Math.min(100, (usage.used / usage.limit) * 100) : 0
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <div className="row" style={{ justifyContent: "space-between" }}><span style={{ fontWeight: 500 }}>{label}</span><span className="mono mut">{usage.used.toLocaleString()}{unit} / {usage.limit.toLocaleString()}{unit}</span></div>
      <div className={`meter ${ratio >= 80 ? "hot" : ""}`} role="progressbar" aria-label={label} aria-valuenow={usage.used} aria-valuemax={usage.limit}><i style={{ width: `${ratio}%` }} /></div>
    </div>
  )
}

/**
 * クォータの画面(自分の上限と使用量)。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function QuotaView({ refreshInterval }: { refreshInterval: number }) {
  const { data, error, mutate } = useSWR<QuotaResponse>("/api/v1/quota", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  if (error && !data) return <ErrorState message="クォータを取得できませんでした。" onRetry={() => void mutate()} />
  if (!data) return <LoadingState />
  return (
    <div className="ws">
      <h1 className="h1 disp" style={{ marginBottom: 16 }}>クォータ</h1>
      <div className="grid g2">
        <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 18 }}>
          <h2 className="h2">リソース</h2>
          <Meter label="プロジェクト" usage={data.projects} />
          <Meter label="デプロイ" usage={data.deploys} />
          <Meter label="ボリューム" usage={data.volumes} />
          <Meter label="ボリュームの合計容量" usage={data.volumeTotalMB} unit=" MB" />
        </div>
        <div className="col">
          <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 18 }}>
            <h2 className="h2">スペックごとのデプロイ</h2>
            {Object.entries(data.deploysBySpec).map(([spec, usage]) => <Meter key={spec} label={spec} usage={usage} />)}
          </div>
          <div className="card pad"><dl className="kv" style={{ margin: 0 }}>
            <div><dt>ボリューム 1つの上限</dt><dd className="mono">{data.maxVolumeSizeMB.toLocaleString()} MB</dd></div>
            <div><dt>レプリカの上限</dt><dd className="mono">{data.maxReplicas}</dd></div>
          </dl></div>
        </div>
      </div>
    </div>
  )
}

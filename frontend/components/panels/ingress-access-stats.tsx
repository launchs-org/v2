"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { AccessRange, AccessStatsResponse } from "@/lib/types"
import { formatBytes } from "@/lib/configmap-files"

const RANGES: { value: AccessRange; label: string }[] = [
  { value: "1h", label: "1 時間" },
  { value: "24h", label: "24 時間" },
  { value: "7d", label: "7 日" },
  { value: "30d", label: "30 日" },
]

/**
 * 数を、読みやすい文字にする(1,234)。
 *
 * @param value 数。
 * @returns 桁区切りの文字列。
 */
function formatCount(value: number): string {
  // 3 桁ごとにカンマを入れる。
  return value.toLocaleString("ja-JP")
}

/**
 * 区間ごとのリクエスト数を、棒グラフ(正常と 5xx の積み上げ)で描く。
 *
 * @param props.series API の series。
 * @returns SVG のグラフ。
 */
function RequestChart({ series }: { series: AccessStatsResponse["series"] }) {
  // 一番大きい区間に高さを合わせる(全部 0 のときは 1)。
  const peak = Math.max(1, ...series.map((point) => point.count))
  const width = 600
  const height = 120
  const barWidth = width / Math.max(1, series.length)
  return (
    <svg viewBox={`0 0 ${width} ${height}`} width="100%" height={height} role="img" aria-label="リクエスト数の推移" preserveAspectRatio="none">
      {series.map((point, index) => {
        const total = (point.count / peak) * (height - 4)
        const failed = (point.status["5xx"] / peak) * (height - 4)
        const x = index * barWidth
        return (
          <g key={point.start}>
            <title>{`${new Date(point.start).toLocaleString("ja-JP")}  ${formatCount(point.count)} 件(5xx: ${point.status["5xx"]})`}</title>
            <rect x={x + 0.5} y={height - total} width={Math.max(1, barWidth - 1)} height={total - failed} fill="var(--accent)" />
            <rect x={x + 0.5} y={height - failed} width={Math.max(1, barWidth - 1)} height={failed} fill="var(--bad)" />
          </g>
        )
      })}
    </svg>
  )
}

/**
 * 上位の一覧(パスやクライアント)を、横棒つきで表示する。
 *
 * @param props.title 見出し。
 * @param props.items キーと件数。
 * @param props.empty 空のときの文言。
 */
function TopList({ title, items, empty }: { title: string; items: { key: string; count: number }[]; empty: string }) {
  // 最大の件数に、棒の長さを合わせる。
  const peak = Math.max(1, ...items.map((item) => item.count))
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <h3 className="h2" style={{ fontSize: 13 }}>{title}</h3>
      {items.length === 0 && <div className="mut" style={{ fontSize: 12.5 }}>{empty}</div>}
      {items.slice(0, 10).map((item) => (
        <div key={item.key} style={{ position: "relative", fontSize: 12.5, padding: "3px 6px", display: "flex", justifyContent: "space-between", gap: 8 }}>
          <span aria-hidden style={{ position: "absolute", inset: 0, width: `${(item.count / peak) * 100}%`, background: "color-mix(in srgb,var(--accent) 14%,transparent)", borderRadius: 4 }} />
          <span className="mono" style={{ position: "relative", overflowWrap: "anywhere" }}>{item.key}</span>
          <span className="mono" style={{ position: "relative" }}>{formatCount(item.count)}</span>
        </div>
      ))}
    </div>
  )
}

/**
 * ingress のアクセスの集計(件数の推移、ステータス、応答時間、上位のパスとクライアント)と、集計の有効・無効の切り替え。
 *
 * @param props.basePath ingress の API のパス(/api/v1/projects/.../ingresses/...)。
 * @param props.enabled 集計が有効か。
 * @param props.locked 反映中・削除中(切り替えられない)。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.onToggle 有効・無効を切り替えるとき。
 */
export function IngressAccessStats({ basePath, enabled, locked, refreshInterval, onToggle }: { basePath: string; enabled: boolean; locked: boolean; refreshInterval: number; onToggle: (next: boolean) => void }) {
  const [range, setRange] = useState<AccessRange>("1h")
  const { data, error } = useSWR<AccessStatsResponse>(`${basePath}/access-stats?range=${range}`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const totals = data?.totals
  const failureRate = totals && totals.count > 0 ? (totals.status["5xx"] / totals.count) * 100 : 0
  return (
    <div className="card" style={{ overflow: "hidden" }}>
      <div className="row pad" style={{ paddingBottom: 8 }}>
        <h2 className="h2 grow">アクセス</h2>
        <label className="row" style={{ gap: 6, fontSize: 12.5 }}>
          <input type="checkbox" checked={enabled} disabled={locked} onChange={(event) => onToggle(event.target.checked)} />
          集計する
        </label>
      </div>
      <div className="mut" style={{ fontSize: 12.5, padding: "0 18px 8px" }}>
        {enabled ? "IP アドレス、パス、メソッド、応答時間を、1 分ごとに集計しています(パスの ID らしい部分は :id にまとめます)。" : "集計は止まっています。過去の集計は、保存期間の間、見られます。"}
      </div>
      <div className="row" style={{ gap: 6, padding: "0 18px 10px" }}>
        {RANGES.map((item) => <button key={item.value} type="button" className={`chip ${range === item.value ? "on" : ""}`} onClick={() => setRange(item.value)}>{item.label}</button>)}
      </div>
      {error && <div style={{ color: "var(--bad)", fontSize: 12.5, padding: "0 18px 12px" }}>集計を取得できませんでした。</div>}
      {data && totals && (
        <div style={{ display: "flex", flexDirection: "column", gap: 14, padding: "0 18px 16px" }}>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(110px,1fr))", gap: 10 }}>
            {[
              ["リクエスト", formatCount(totals.count)],
              ["5xx の割合", `${failureRate.toFixed(1)}%`],
              ["応答時間(平均)", `${totals.avgMs.toFixed(0)} ms`],
              ["応答時間(p95)", `≤ ${totals.p95Ms} ms`],
              ["転送量", formatBytes(totals.bytes)],
              ["クライアント", data.uniqueClients ? `${formatCount(data.uniqueClients)}` : "—"],
            ].map(([label, value]) => (
              <div key={label} style={{ display: "flex", flexDirection: "column" }}>
                <span className="faint" style={{ fontSize: 11.5 }}>{label}</span>
                <span className="mono" style={{ fontWeight: 600 }}>{value}</span>
              </div>
            ))}
          </div>
          <RequestChart series={data.series} />
          <div className="mut" style={{ fontSize: 12 }}>
            {(["2xx", "3xx", "4xx", "5xx"] as const).map((name) => `${name}: ${formatCount(totals.status[name])}`).join("  ·  ")}
            {Object.keys(totals.methods).length > 0 && `   |   ${Object.entries(totals.methods).map(([name, count]) => `${name}: ${formatCount(count)}`).join("  ·  ")}`}
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(220px,1fr))", gap: 16 }}>
            <TopList title="よく使われたパス" items={data.paths} empty="まだ集計がありません。" />
            <TopList title="よく来たクライアント(IP)" items={data.clients} empty="記録がありません。" />
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * 集計の有効・無効を、API に送る。
 *
 * @param basePath ingress の API のパス。
 * @param next 有効にするなら true。
 */
export async function updateAccessStats(basePath: string, next: boolean) {
  // PATCH で切り替える(サーバーが、IngressRoute の作り直しを始める)。
  await api.patch(basePath, { accessStats: next })
}

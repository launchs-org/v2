"use client"

import { useState } from "react"
import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import type { MetricPoint, MetricsResponse } from "@/lib/types"
import { ErrorState, LoadingState } from "@/components/ui-kit"

/** 選べる範囲。 */
const RANGES: { key: "1h" | "6h" | "24h"; label: string }[] = [{ key: "1h", label: "1 時間" }, { key: "6h", label: "6 時間" }, { key: "24h", label: "24 時間" }]

/** グラフの大きさ(viewBox。実際の幅は、親に合わせて伸びる)。 */
const CHART_WIDTH = 600
const CHART_HEIGHT = 110

/**
 * 折れ線のグラフ(SVG)。値が無い(null)区間は、線を切る。縦軸は 0 から、最大値の少し上まで。
 *
 * @param props.title グラフの見出し。
 * @param props.points 時刻(ミリ秒)と値(測れなかったら null)。
 * @param props.format 値を、文字にする関数(最新の値と、最大の値の表示に使う)。
 * @param props.color 線の色(CSS の色)。
 */
function LineChart({ title, points, format, color }: { title: string; points: { time: number; value: number | null }[]; format: (value: number) => string; color: string }) {
  // 測れた値だけを集めて、最大と最新を決める。
  const measured = points.filter((point) => point.value !== null) as { time: number; value: number }[]
  const maximum = Math.max(1, ...measured.map((point) => point.value)) * 1.15
  const first = points[0]?.time ?? 0
  const span = Math.max(1, (points[points.length - 1]?.time ?? 1) - first)
  // 値を、グラフの座標にする。
  const toX = (time: number) => ((time - first) / span) * CHART_WIDTH
  const toY = (value: number) => CHART_HEIGHT - (value / maximum) * (CHART_HEIGHT - 8) - 4
  // null で線を切りながら、折れ線の path を作る。
  let path = ""
  let drawing = false
  for (const point of points) {
    if (point.value === null) { drawing = false; continue }
    path += `${drawing ? "L" : "M"}${toX(point.time).toFixed(1)},${toY(point.value).toFixed(1)} `
    drawing = true
  }
  const latest = measured[measured.length - 1]
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div className="row" style={{ alignItems: "baseline" }}>
        <h2 className="h2 grow">{title}</h2>
        <span className="mono" style={{ fontSize: 13 }}>{latest ? `いま ${format(latest.value)}` : "データなし"}</span>
        {measured.length > 0 && <span className="faint mono" style={{ fontSize: 12, marginLeft: 12 }}>最大 {format(Math.max(...measured.map((point) => point.value)))}</span>}
      </div>
      <svg viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`} preserveAspectRatio="none" role="img" aria-label={title} style={{ width: "100%", height: 110, display: "block" }}>
        <line x1="0" y1={CHART_HEIGHT - 4} x2={CHART_WIDTH} y2={CHART_HEIGHT - 4} stroke="var(--border, #8884)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        <path d={path} fill="none" stroke={color} strokeWidth="1.8" vectorEffect="non-scaling-stroke" />
      </svg>
    </div>
  )
}

/** CPU(ミリコア)を、読みやすい文字にする(1000 以上はコア)。 */
function formatCpu(millicores: number): string {
  return millicores >= 1000 ? `${(millicores / 1000).toFixed(2)} コア` : `${Math.round(millicores)} m`
}

/** メモリ(バイト)を、読みやすい文字にする。 */
function formatMemory(bytes: number): string {
  const mebibytes = bytes / 1024 / 1024
  return mebibytes >= 1024 ? `${(mebibytes / 1024).toFixed(2)} GiB` : `${mebibytes.toFixed(0)} MiB`
}

/**
 * メトリクスの表示(CPU、メモリ、Pod の数、再起動)。範囲(1/6/24 時間)を選べる。自動で更新する。
 * deploy 1 つ、プロジェクト全体、タグのどれでも、API のパスを変えるだけで使える。
 *
 * @param props.path メトリクスを取る API の、`/metrics` の手前のパス(例: `/api/v1/deploy/xxx`、`/api/v1/projects/xxx/tags/web`)。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。0 なら止める)。
 */
export function MetricsPanel({ path, refreshInterval }: { path: string; refreshInterval: number }) {
  const [range, setRange] = useState<"1h" | "6h" | "24h">("1h")
  const { data, error, mutate } = useSWR<MetricsResponse>(`${path}/metrics?range=${range}`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true, refreshWhenHidden: false })
  if (error && !data) return <ErrorState message="メトリクスを取得できませんでした" onRetry={() => void mutate()} />
  if (!data) return <LoadingState />
  const points: MetricPoint[] = data.points
  const times = points.map((point) => new Date(point.at).getTime())
  // CPU とメモリが、1 つも測れていないとき(metrics-server が無い)は、理由を出す。
  const hasUsage = points.some((point) => point.cpuMillicores !== null || point.memoryBytes !== null)
  return (
    <div className="col" style={{ gap: 12 }}>
      <div className="row" style={{ gap: 6 }}>
        {RANGES.map((item) => <button key={item.key} type="button" className={`btn sm ${range === item.key ? "primary" : ""}`} onClick={() => setRange(item.key)}>{item.label}</button>)}
        <span className="grow" />
        <span className="faint" style={{ fontSize: 12 }}>30 秒ごとに記録</span>
      </div>
      {points.length === 0 && <div className="mut">まだ、記録がありません。起動してから、30 秒ほどで、最初の記録ができます。</div>}
      {points.length > 0 && !hasUsage && <div className="mut" style={{ fontSize: 12.5 }}>このクラスターには metrics-server が無いため、CPU とメモリは記録されていません(Pod の数と再起動だけ)。</div>}
      {points.length > 0 && (
        <>
          <LineChart title="CPU" color="var(--accent, #4f8ef7)" format={formatCpu} points={points.map((point, index) => ({ time: times[index], value: point.cpuMillicores }))} />
          <LineChart title="メモリ" color="var(--ok, #3fb68b)" format={formatMemory} points={points.map((point, index) => ({ time: times[index], value: point.memoryBytes }))} />
          <LineChart title="Pod の数" color="var(--warn, #d9a441)" format={(value) => `${Math.round(value)} 個`} points={points.map((point, index) => ({ time: times[index], value: point.pods }))} />
          <LineChart title="再起動の回数(合計)" color="var(--bad, #e5534b)" format={(value) => `${Math.round(value)} 回`} points={points.map((point, index) => ({ time: times[index], value: point.restarts }))} />
        </>
      )}
    </div>
  )
}

"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import { Badge, ErrorState, LoadingState } from "@/components/ui-kit"

type AdminDeploy = { deployId: string; projectId: string; projectName: string; userId: string; imageRef: string; spec: string; replicas: number; status: string; createdAt: string }
type DeployList = { deploys: AdminDeploy[]; nextCursor: string | null }

/** 状態の選択肢。 */
const STATUSES = ["pending", "active", "applying", "failed", "scaled_zero", "deleting", "delete_failed"]

/** 行の列の幅。 */
const COLUMNS = "1.2fr 1.4fr 1.2fr 80px 90px 120px 150px"

/**
 * 全ユーザーの deploy の一覧(読むだけ)。状態、持ち主のユーザー ID、キーワード(イメージ、プロジェクト名、ID)で絞り込める。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function AdminDeploys({ refreshInterval }: { refreshInterval: number }) {
  const [status, setStatus] = useState("")
  const [keyword, setKeyword] = useState("")
  const query = `limit=50${status ? `&status=${status}` : ""}${keyword.trim() ? `&q=${encodeURIComponent(keyword.trim())}` : ""}`
  const { data, error, mutate } = useSWR<DeployList>(`/admin-api/deploys?${query}`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  // 「もっと見る」で足したページ。
  const [more, setMore] = useState<AdminDeploy[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor

  /** 絞り込みを変えたとき、足したページを捨てる。 */
  function resetPaging() {
    setMore([])
    setCursor(undefined)
  }

  /** 次のページを読み足す。 */
  async function loadMore() {
    if (!nextCursor) return
    const page = await api.get<DeployList>(`/admin-api/deploys?${query}&cursor=${encodeURIComponent(nextCursor)}`)
    setMore((current) => [...current, ...page.deploys])
    setCursor(page.nextCursor)
  }

  const deploys = data ? [...data.deploys, ...more] : []
  const cols = { "--cols": COLUMNS } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 980 }}>
      <div className="pad row" style={{ paddingBottom: 8, gap: 10, flexWrap: "wrap" }}>
        <h2 className="h2 grow">デプロイメント</h2>
        <select className="inp" style={{ width: "auto" }} value={status} onChange={(event) => { setStatus(event.target.value); resetPaging() }} aria-label="状態で絞り込む">
          <option value="">すべての状態</option>{STATUSES.map((item) => <option key={item} value={item}>{item}</option>)}
        </select>
        <input className="inp" style={{ width: 220 }} value={keyword} onChange={(event) => { setKeyword(event.target.value); resetPaging() }} placeholder="イメージ・プロジェクト名・ID" aria-label="キーワードで絞り込む" />
      </div>
      <div className="tr th" style={cols}><span>deploy</span><span>イメージ</span><span>プロジェクト</span><span>スペック</span><span>レプリカ</span><span>状態</span><span>作成</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="deploy を取得できませんでした" onRetry={() => void mutate()} /></div>}
      {data && deploys.length === 0 && <div className="pad mut">deploy はありません。</div>}
      {deploys.map((deploy) => (
        <div key={deploy.deployId} className="tr" style={cols}>
          <span className="mono" style={{ fontSize: 11.5, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }} title={deploy.userId}>{deploy.deployId}<div className="faint">{deploy.userId}</div></span>
          <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }} title={deploy.imageRef}>{deploy.imageRef}</span>
          <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>{deploy.projectName || <span className="faint">(削除済み)</span>}</span>
          <span>{deploy.spec}</span>
          <span>{deploy.replicas}</span>
          <span><Badge status={deploy.status} /></span>
          <span className="mono faint">{shortDateTime(deploy.createdAt)}</span>
        </div>
      ))}
      {nextCursor && <div className="pad"><button className="btn" type="button" onClick={() => void loadMore()}>もっと見る</button></div>}
    </div></div>
  )
}

"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ApiTokenCreateResponse, ApiTokenListResponse } from "@/lib/types"
import { tokenState, TokenRevealModal } from "@/components/api-token-section"

/**
 * deploy の「自動デプロイ(webhook)」の欄。CI やレジストリが webhook を呼ぶと、イメージを更新して反映する。
 * この deploy のイメージの更新だけを許すトークン(deploy:image)を、発行・失効する。
 *
 * @param props.deployId 対象の deploy の ID。
 * @param props.locked 反映中などで、操作できないとき(発行は、できる。ここでは使わない)。
 */
export function DeployWebhookCard({ deployId }: { deployId: string }) {
  const { data, mutate } = useSWR<ApiTokenListResponse>("/api/v1/api-tokens", authenticatedFetcher, { revalidateOnFocus: true })
  const [busy, setBusy] = useState(false)
  const [issued, setIssued] = useState<string | null>(null)
  const [message, setMessage] = useState("")
  // この deploy 用の、有効なトークン。
  const mine = (data?.tokens ?? []).filter((token) => token.scope.length === 1 && token.scope[0] === "deploy:image" && token.deploys.includes(deployId) && tokenState(token) === "active")
  const url = typeof window === "undefined" ? "" : `${window.location.origin}/hooks/deploy`

  /** この deploy 用のトークンを発行して、本体を1度だけ見せる。 */
  async function create() {
    setBusy(true)
    setMessage("")
    try {
      const result = await api.post<ApiTokenCreateResponse>("/api/v1/api-tokens", { name: `webhook ${deployId.slice(0, 15)}`, scope: ["deploy:image"], deploys: [deployId] })
      setIssued(result.token)
      await mutate()
    } catch (createError) {
      setMessage(createError instanceof Error ? createError.message : "発行できませんでした")
    } finally {
      setBusy(false)
    }
  }

  /** トークンを失効させる(その webhook は、使えなくなる)。 */
  async function revoke(tokenId: string) {
    if (!window.confirm("この webhook のトークンを失効させますか? 呼び出し側の設定も、直す必要があります。")) return
    try {
      await api.delete(`/api/v1/api-tokens/${encodeURIComponent(tokenId)}`)
      await mutate()
    } catch (revokeError) {
      setMessage(revokeError instanceof Error ? revokeError.message : "失効できませんでした")
    }
  }

  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div className="row"><h2 className="h2 grow">自動デプロイ(webhook)</h2><button className="btn sm" type="button" disabled={busy} onClick={() => void create()}>トークンを発行</button></div>
      <div className="mut" style={{ fontSize: 13 }}>CI やレジストリから、下の URL を呼ぶと、イメージを更新して反映します。トークンは、この deploy のイメージの更新だけができます。</div>
      <div className="mono" style={{ fontSize: 12, wordBreak: "break-all" }}>POST {url}{"\n"}X-Launchs-Token: &lt;トークン&gt;{"\n"}{`{"tag": "v2"}`} または {`{"imageRef": "ghcr.io/org/app:v2"}`}</div>
      {mine.map((token) => <div key={token.tokenId} className="opt" style={{ cursor: "default" }}><span className="mono faint">{token.tokenId.slice(0, 17)}…</span><span className="grow" /><span className="faint" style={{ fontSize: 12 }}>{token.lastUsedAt ? "使用あり" : "未使用"}</span><button className="btn sm danger" type="button" onClick={() => void revoke(token.tokenId)}>失効</button></div>)}
      {message && <div className="mut" role="alert" style={{ color: "var(--bad, #c0392b)" }}>{message}</div>}
      {issued && <TokenRevealModal token={issued} onClose={() => setIssued(null)}
        hint={<div className="mono faint" style={{ fontSize: 12, marginTop: 10, wordBreak: "break-all" }}>{`curl -X POST ${url} -H "X-Launchs-Token: ${issued}" -d '{"tag":"v2"}'`}</div>} />}
    </div>
  )
}

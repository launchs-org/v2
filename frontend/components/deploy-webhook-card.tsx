"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import type { ApiTokenCreateResponse, ApiTokenListResponse } from "@/lib/types"
import { CopyButton, TokenRevealModal, tokenState } from "@/components/api-token-section"
import { Modal } from "@/components/ui-kit"
import { GithubActionsGuide } from "@/components/github-actions-guide"

/**
 * deploy の「自動デプロイ(webhook)」の欄。トークンは、あとから、もう一度見られる。GitHub Actions の workflow も作れる。CI やレジストリが webhook を呼ぶと、イメージを更新して反映する。
 * この deploy のイメージの更新だけを許すトークン(deploy:image)を、発行・失効する。
 *
 * @param props.deployId 対象の deploy の ID。
 */
export function DeployWebhookCard({ deployId }: { deployId: string }) {
  const { data, mutate } = useSWR<ApiTokenListResponse>("/api/v1/api-tokens", authenticatedFetcher, { revalidateOnFocus: true })
  const [busy, setBusy] = useState(false)
  const [issued, setIssued] = useState<string | null>(null)
  const [revokingId, setRevokingId] = useState<string | null>(null)
  const [message, setMessage] = useState("")
  const [showWorkflow, setShowWorkflow] = useState(false)
  // この deploy 用の、有効なトークン。
  const mine = (data?.tokens ?? []).filter((token) => token.scope.length === 1 && token.scope[0] === "deploy:image" && token.deploys.includes(deployId) && tokenState(token) === "active")
  const url = typeof window === "undefined" ? "" : `${window.location.origin}/hooks/deploy`
  const example = (token: string) => `curl -X POST ${url} \\\n  -H "X-Launchs-Token: ${token}" \\\n  -d '{"tag":"v2"}'`

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

  /** 発行済みのトークンの本体を、もう一度表示する(deploy 用のトークンだけ、サーバーが作り直す)。 */
  async function reveal(tokenId: string) {
    setMessage("")
    try {
      const result = await api.get<{ token: string }>(`/api/v1/api-tokens/${encodeURIComponent(tokenId)}/reveal`)
      setIssued(result.token)
    } catch (revealError) {
      setMessage(revealError instanceof Error ? revealError.message : "トークンを取得できませんでした")
    }
  }

  /** トークンを失効させる(その webhook は、使えなくなる)。 */
  async function revoke() {
    if (!revokingId) return
    setBusy(true)
    try {
      await api.delete(`/api/v1/api-tokens/${encodeURIComponent(revokingId)}`)
      setRevokingId(null)
      await mutate()
    } catch (revokeError) {
      setMessage(revokeError instanceof Error ? revokeError.message : "失効できませんでした")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card pad col" style={{ gap: 12 }}>
      <div className="row"><h2 className="h2 grow">自動デプロイ(webhook)</h2><button className="btn sm" type="button" onClick={() => setShowWorkflow(true)}>GitHub Actions で自動デプロイ</button><button className="btn sm primary" type="button" disabled={busy} onClick={() => void create()}>トークンを発行</button></div>
      <div className="mut" style={{ fontSize: 13 }}>CI やレジストリから webhook を呼ぶと、イメージを更新して反映します。トークンは、この deploy のイメージの更新だけができます。</div>
      <div className="col" style={{ gap: 6 }}>
        <div className="row"><b style={{ fontSize: 13 }}>呼び出し方</b><span className="grow" /><CopyButton text={example("<トークン>")} /></div>
        <div className="codebox">{example("<トークン>")}</div>
        <div className="faint" style={{ fontSize: 12 }}>ボディは {`{"tag":"v2"}`}(いまのイメージのタグだけ差し替え)か {`{"imageRef":"ghcr.io/org/app:v2"}`}。同じイメージなら、何も起きません。</div>
      </div>
      {mine.length > 0 && (
        <div className="col" style={{ gap: 6 }}>
          <b style={{ fontSize: 13 }}>発行済みのトークン</b>
          {mine.map((token) => (
            <div key={token.tokenId} className="opt" style={{ cursor: "default" }}>
              <span className="badge b-ok"><i />有効</span>
              <span className="mono faint" style={{ fontSize: 12 }}>{token.tokenId.slice(0, 18)}…</span>
              <span className="grow" />
              <span className="faint" style={{ fontSize: 12 }}>{token.lastUsedAt ? `最終使用 ${shortDateTime(token.lastUsedAt)}` : "未使用"}</span>
              <button className="btn sm" type="button" onClick={() => void reveal(token.tokenId)}>トークンを表示</button>
              <button className="btn sm danger" type="button" onClick={() => setRevokingId(token.tokenId)}>失効</button>
            </div>
          ))}
        </div>
      )}
      {message && <div className="err" role="alert">{message}</div>}
      {issued && <TokenRevealModal token={issued} example={example(issued)} reusable onClose={() => setIssued(null)} />}
      {showWorkflow && <GithubActionsGuide deployId={deployId} onClose={() => setShowWorkflow(false)} />}
      {revokingId && (
        <Modal title="この webhook のトークンを失効させますか?" description="このトークンは、すぐに使えなくなります。呼び出し側(CI など)の設定も、新しいトークンに直す必要があります。" onClose={() => setRevokingId(null)}
          footer={<><button className="btn" type="button" onClick={() => setRevokingId(null)}>キャンセル</button><button className="btn danger" type="button" disabled={busy} onClick={() => void revoke()}>失効させる</button></>}><span /></Modal>
      )}
    </div>
  )
}

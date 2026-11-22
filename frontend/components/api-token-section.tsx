"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import type { ApiToken, ApiTokenCreateResponse, ApiTokenListResponse } from "@/lib/types"
import { ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"

/** scope を、画面に出す言葉にする。 */
const SCOPE_LABEL: Record<string, string> = { read: "読み取り", write: "全ての操作", "deploy:image": "イメージの更新" }

/** トークンの状態(有効、失効、期限切れ)を、言葉にする。 */
export function tokenState(token: ApiToken): "active" | "revoked" | "expired" {
  if (token.revokedAt) return "revoked"
  if (token.expiresAt && new Date(token.expiresAt).getTime() < Date.now()) return "expired"
  return "active"
}

/** 発行したトークンを、1度だけ見せる画面(あとからは、見られない)。 */
export function TokenRevealModal({ token, hint, onClose }: { token: string; hint?: React.ReactNode; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  return (
    <Modal title="トークンを発行しました" description="この画面を閉じると、トークンは、二度と表示できません。いま、安全な場所に保存してください。" onClose={onClose}
      footer={<><button className="btn" type="button" onClick={() => { void navigator.clipboard.writeText(token).then(() => setCopied(true)) }}>{copied ? "コピーしました" : "トークンをコピー"}</button><button className="btn primary" type="button" onClick={onClose}>閉じる</button></>}>
      <div className="mono" style={{ wordBreak: "break-all", userSelect: "all" }}>{token}</div>
      {hint}
    </Modal>
  )
}

/**
 * アカウント設定の「API トークン」の欄。機械(CI、スクリプト)が、自分の代わりに API を呼ぶためのトークンを、発行・一覧・失効する。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function ApiTokenSection({ refreshInterval }: { refreshInterval: number }) {
  const { data, error, mutate } = useSWR<ApiTokenListResponse>("/api/v1/api-tokens", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [name, setName] = useState("")
  const [scope, setScope] = useState("read")
  const [days, setDays] = useState("90")
  const [busy, setBusy] = useState(false)
  const [issued, setIssued] = useState<string | null>(null)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  /** トークンを発行して、本体を1度だけ見せる。 */
  async function create() {
    setBusy(true)
    try {
      const body: Record<string, unknown> = { name: name.trim(), scope: [scope] }
      if (days !== "") body.expiresInDays = Number(days)
      const result = await api.post<ApiTokenCreateResponse>("/api/v1/api-tokens", body)
      setIssued(result.token)
      setName("")
      await mutate()
    } catch (createError) {
      setToast({ message: createError instanceof Error ? createError.message : "発行できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  /** トークンを失効させる(使えなくなる。元には戻せない)。 */
  async function revoke(token: ApiToken) {
    if (!window.confirm(`トークン「${token.name}」を失効させますか? このトークンは使えなくなります。`)) return
    try {
      await api.delete(`/api/v1/api-tokens/${encodeURIComponent(token.tokenId)}`)
      setToast({ message: "失効させました", type: "success" })
      await mutate()
    } catch (revokeError) {
      setToast({ message: revokeError instanceof Error ? revokeError.message : "失効できませんでした", type: "error" })
    }
  }

  // deploy 用のトークン(webhook)は、各 deploy の画面で管理するので、ここでは、全体のトークンだけを出す。
  const tokens = (data?.tokens ?? []).filter((token) => !(token.scope.length === 1 && token.scope[0] === "deploy:image"))
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h2 className="h2">API トークン</h2>
      <div className="mut" style={{ fontSize: 13 }}>CI やスクリプトが、あなたの代わりに API を呼ぶためのトークンです。<span className="mono"> Authorization: Bearer &lt;トークン&gt; </span>で渡します。トークンの管理は、ログインしたときだけできます。</div>
      <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
        <input className="input" placeholder="名前(用途のメモ)" value={name} maxLength={100} onChange={(event) => setName(event.target.value)} style={{ minWidth: 200 }} />
        <select className="input" value={scope} onChange={(event) => setScope(event.target.value)}><option value="read">読み取りだけ</option><option value="write">全ての操作</option></select>
        <select className="input" value={days} onChange={(event) => setDays(event.target.value)}><option value="30">30 日</option><option value="90">90 日</option><option value="365">1 年</option><option value="">無期限</option></select>
        <button className="btn primary" type="button" disabled={busy || name.trim() === ""} onClick={() => void create()}>発行</button>
      </div>
      {!data && !error && <LoadingState />}
      {error && !data && <ErrorState message="トークンを取得できませんでした。" onRetry={() => void mutate()} />}
      {tokens.map((token) => {
        const state = tokenState(token)
        return (
          <div key={token.tokenId} className="opt" style={{ cursor: "default" }}>
            <span style={{ color: "var(--text)" }}>{token.name}</span>
            <span className="faint">{token.scope.map((item) => SCOPE_LABEL[item] ?? item).join("・")}</span>
            <span className="grow" />
            <span className="faint" style={{ fontSize: 12 }}>{state === "revoked" ? "失効" : state === "expired" ? "期限切れ" : token.expiresAt ? `〜${shortDateTime(token.expiresAt)}` : "無期限"}{token.lastUsedAt ? ` ・ 最終 ${shortDateTime(token.lastUsedAt)}` : " ・ 未使用"}</span>
            {state === "active" && <button className="btn sm danger" type="button" onClick={() => void revoke(token)}>失効</button>}
          </div>
        )
      })}
      {data && tokens.length === 0 && <div className="mut">トークンはありません。</div>}
      {issued && <TokenRevealModal token={issued} onClose={() => setIssued(null)} />}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div>
  )
}

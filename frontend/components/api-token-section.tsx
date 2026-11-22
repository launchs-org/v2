"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import type { ApiToken, ApiTokenCreateResponse, ApiTokenListResponse } from "@/lib/types"
import { ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"

/** 権限の選択肢(名前と、できることの説明)。 */
const SCOPE_CHOICES = [
  { value: "read", title: "読み取り専用", description: "一覧や詳細を見るだけ(GET)。状態の監視や、ダッシュボード向け" },
  { value: "write", title: "全ての操作", description: "作成、更新、削除も含めて、あなたと同じ操作ができる(トークンの管理を除く)" },
] as const

/** 期限の選択肢。 */
const EXPIRY_CHOICES = [{ value: "30", label: "30 日" }, { value: "90", label: "90 日" }, { value: "365", label: "1 年" }, { value: "", label: "無期限" }] as const

/** scope を、画面に出す短い言葉にする。 */
const SCOPE_LABEL: Record<string, string> = { read: "読み取り", write: "全ての操作", "deploy:image": "イメージの更新" }

/** トークンの状態(有効、失効、期限切れ)。 */
export function tokenState(token: ApiToken): "active" | "revoked" | "expired" {
  if (token.revokedAt) return "revoked"
  if (token.expiresAt && new Date(token.expiresAt).getTime() < Date.now()) return "expired"
  return "active"
}

/** 文字列をクリップボードに写す小さなボタン。 */
export function CopyButton({ text, label = "コピー" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  return <button className="btn sm" type="button" onClick={() => { void navigator.clipboard.writeText(text).then(() => { setCopied(true); window.setTimeout(() => setCopied(false), 1800) }) }}>{copied ? "コピーしました" : label}</button>
}

/**
 * トークンを見せる画面。権限の広いトークン(既定)は、発行のときの 1 度しか見られない。
 * 再表示できるもの(deploy 用)は、reusable を渡して、そう伝える。
 *
 * @param props.token トークンの本体。
 * @param props.example 使い方の例。
 * @param props.reusable あとから、もう一度見られるときは true(deploy 用のトークン)。
 */
export function TokenRevealModal({ token, example, reusable = false, onClose }: { token: string; example?: string; reusable?: boolean; onClose: () => void }) {
  return (
    <Modal title={reusable ? "トークン" : "トークンを発行しました"} description={reusable ? "このトークンは、deploy の「自動デプロイ」の「トークンを表示」で、あとから、もう一度見られます。漏れたときは、失効してください。" : "この画面を閉じると、トークンは二度と表示できません。いま、安全な場所に保存してください。"} onClose={onClose}
      footer={<button className="btn primary" type="button" onClick={onClose}>{reusable ? "閉じる" : "保存したので閉じる"}</button>}>
      <div className="col" style={{ gap: 14 }}>
        <div className="col" style={{ gap: 6 }}>
          <div className="row"><b style={{ fontSize: 13 }}>トークン</b><span className="grow" /><CopyButton text={token} /></div>
          <div className="codebox">{token}</div>
        </div>
        {example && (
          <div className="col" style={{ gap: 6 }}>
            <div className="row"><b style={{ fontSize: 13 }}>使い方の例</b><span className="grow" /><CopyButton text={example} /></div>
            <div className="codebox">{example}</div>
          </div>
        )}
      </div>
    </Modal>
  )
}

/**
 * 「API トークン」の欄。機械(CI、スクリプト)が、自分の代わりに API を呼ぶためのトークンを、発行・一覧・失効する。
 * projectId を渡すと、そのプロジェクトだけを操作できるトークンを扱う(プロジェクトの設定の画面用)。渡さないときは、アカウント全体用。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 * @param props.projectId 対象のプロジェクトの ID(省略なら、アカウント全体)。
 */
export function ApiTokenSection({ refreshInterval, projectId }: { refreshInterval: number; projectId?: string }) {
  const { data, error, mutate } = useSWR<ApiTokenListResponse>("/api/v1/api-tokens", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState("")
  const [scope, setScope] = useState<string>("read")
  const [days, setDays] = useState("90")
  const [busy, setBusy] = useState(false)
  const [issued, setIssued] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<ApiToken | null>(null)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  /** トークンを発行して、本体を1度だけ見せる。 */
  async function create() {
    setBusy(true)
    try {
      const body: Record<string, unknown> = { name: name.trim(), scope: [scope] }
      if (days !== "") body.expiresInDays = Number(days)
      // プロジェクトの画面からは、そのプロジェクトだけに絞る。
      if (projectId) body.projects = [projectId]
      const result = await api.post<ApiTokenCreateResponse>("/api/v1/api-tokens", body)
      setCreating(false)
      setName("")
      setIssued(result.token)
      await mutate()
    } catch (createError) {
      setToast({ message: createError instanceof Error ? createError.message : "発行できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  /** トークンを失効させる(使えなくなる。元には戻せない)。 */
  async function revoke() {
    if (!revoking) return
    setBusy(true)
    try {
      await api.delete(`/api/v1/api-tokens/${encodeURIComponent(revoking.tokenId)}`)
      setRevoking(null)
      setToast({ message: "失効させました", type: "success" })
      await mutate()
    } catch (revokeError) {
      setToast({ message: revokeError instanceof Error ? revokeError.message : "失効できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  // deploy 用のトークン(webhook)は、各 deploy の画面で管理するので、ここには出さない。
  // プロジェクトの画面では、そのプロジェクトに絞ったトークンだけを出す。アカウントの画面では全て(絞ったものには、対象を添える)。
  const tokens = (data?.tokens ?? []).filter((token) => !(token.scope.length === 1 && token.scope[0] === "deploy:image") && (!projectId || token.projects.includes(projectId)))
  const columns = { "--cols": "1.4fr 120px 1fr 130px 130px 90px 70px" } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner">
      <div className="pad col" style={{ gap: 6, paddingBottom: 12 }}>
        <div className="row">
          <h2 className="h2 grow">API トークン{projectId ? "(このプロジェクト)" : ""}</h2>
          <button className="btn primary" type="button" onClick={() => setCreating(true)}>トークンを発行</button>
        </div>
        <div className="mut" style={{ fontSize: 13 }}>
          {projectId ? "このプロジェクトの資源だけを操作できます。他のプロジェクトには使えません。" : "CI やスクリプトが、あなたの代わりに API を呼ぶためのトークンです。プロジェクトだけに絞ったものは、各プロジェクトの「設定」で発行します。"}
          {" "}<span className="mono">Authorization: Bearer &lt;トークン&gt;</span> で渡します。
        </div>
      </div>
      <div className="tr th" style={columns}><span>名前</span><span>権限</span><span>対象</span><span>期限</span><span>最終使用</span><span>状態</span><span /></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="トークンを取得できませんでした。" onRetry={() => void mutate()} /></div>}
      {tokens.map((token) => {
        const state = tokenState(token)
        return (
          <div key={token.tokenId} className="tr" style={columns}>
            <span style={{ fontWeight: 500, wordBreak: "break-all" }}>{token.name}</span>
            <span><span className="badge b-info">{token.scope.map((item) => SCOPE_LABEL[item] ?? item).join("・")}</span></span>
            <span className="mut" style={{ fontSize: 13 }}>{token.projects.length > 0 ? `プロジェクト ${token.projects.length} 件` : "全てのプロジェクト"}</span>
            <span className="mono faint" style={{ fontSize: 12 }}>{token.expiresAt ? shortDateTime(token.expiresAt) : "無期限"}</span>
            <span className="mono faint" style={{ fontSize: 12 }}>{token.lastUsedAt ? shortDateTime(token.lastUsedAt) : "未使用"}</span>
            <span><span className={`badge ${state === "active" ? "b-ok" : state === "expired" ? "b-warn" : "b-dim"}`}><i />{state === "active" ? "有効" : state === "expired" ? "期限切れ" : "失効"}</span></span>
            <span>{state === "active" && <button className="btn sm danger" type="button" onClick={() => setRevoking(token)}>失効</button>}</span>
          </div>
        )
      })}
      {data && tokens.length === 0 && <div className="tr mut" style={{ "--cols": "1fr" } as React.CSSProperties}>トークンはまだありません。「トークンを発行」から作れます。</div>}

      {creating && (
        <Modal title="API トークンを発行" description={projectId ? "このプロジェクトだけを操作できるトークンを作ります。" : "全てのプロジェクトを操作できるトークンを作ります。"} onClose={() => setCreating(false)}
          footer={<><button className="btn" type="button" onClick={() => setCreating(false)}>キャンセル</button><button className="btn primary" type="button" disabled={busy || name.trim() === ""} onClick={() => void create()}>発行</button></>}>
          <div className="col" style={{ gap: 16 }}>
            <label className="lbl">名前(用途のメモ)<input className="inp" value={name} maxLength={100} placeholder="例: GitHub Actions" onChange={(event) => setName(event.target.value)} autoFocus /></label>
            <div className="col" style={{ gap: 8 }}>
              <span className="lbl" style={{ gap: 0 }}>権限</span>
              {SCOPE_CHOICES.map((choice) => <button key={choice.value} type="button" className={`scopecard ${scope === choice.value ? "on" : ""}`} onClick={() => setScope(choice.value)}><b>{choice.title}</b><span>{choice.description}</span></button>)}
            </div>
            <div className="col" style={{ gap: 8 }}>
              <span className="lbl" style={{ gap: 0 }}>有効期間</span>
              <div className="seg">{EXPIRY_CHOICES.map((choice) => <button key={choice.label} type="button" className={days === choice.value ? "on" : ""} onClick={() => setDays(choice.value)}>{choice.label}</button>)}</div>
            </div>
          </div>
        </Modal>
      )}
      {revoking && (
        <Modal title={`「${revoking.name}」を失効させますか?`} description="このトークンは、すぐに使えなくなります。使っている CI やスクリプトは、動かなくなります。元に戻せません。" onClose={() => setRevoking(null)}
          footer={<><button className="btn" type="button" onClick={() => setRevoking(null)}>キャンセル</button><button className="btn danger" type="button" disabled={busy} onClick={() => void revoke()}>失効させる</button></>}><span /></Modal>
      )}
      {issued && <TokenRevealModal token={issued} example={`curl -H "Authorization: Bearer ${issued}" ${typeof window === "undefined" ? "" : window.location.origin}/api/v1/projects`} onClose={() => setIssued(null)} />}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div></div>
  )
}

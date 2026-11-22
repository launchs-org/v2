"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import type { Invitation, InvitationCreateResponse, InvitationListResponse, InvitationStatus } from "@/lib/types"
import { CopyButton } from "@/components/api-token-section"
import { ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"

/** 有効期間の選択肢(時間で渡す。上限は 720 時間 = 30 日)。 */
const EXPIRY_CHOICES = [{ value: "24", label: "1 日" }, { value: "168", label: "7 日" }, { value: "720", label: "30 日" }] as const

/** 状態を、画面に出す言葉と、バッジの色にする。 */
const STATUS_VIEW: Record<InvitationStatus, { label: string; badge: string }> = {
  active: { label: "有効", badge: "b-ok" },
  used: { label: "使用済み", badge: "b-dim" },
  expired: { label: "期限切れ", badge: "b-dim" },
  revoked: { label: "取り消し済み", badge: "b-bad" },
}

/**
 * 招待の管理(管理者だけが開く)。招待コード(1 回だけ使える)を発行し、一覧で状態を見て、有効なものを取り消せる。
 * コードは、発行のときの 1 度しか見られない(サーバーは、ハッシュしか持たない)。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function InvitationSection({ refreshInterval }: { refreshInterval: number }) {
  // 招待の一覧(管理者の API。管理者でなければ、サーバーが 403 を返す)。
  const { data, error, mutate } = useSWR<InvitationListResponse>("/auth/admin/invitations?limit=50", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  // 発行の画面を開いているか。
  const [creating, setCreating] = useState(false)
  // 発行の入力(有効期間と、メモ)。
  const [expiresInHours, setExpiresInHours] = useState("168")
  const [note, setNote] = useState("")
  // 発行した直後のコード(閉じると消える)。
  const [issued, setIssued] = useState<InvitationCreateResponse | null>(null)
  // 取り消しの確認中の招待。
  const [revoking, setRevoking] = useState<Invitation | null>(null)
  // 処理中か、知らせ。
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  /** 招待コードを発行して、コードを見せる画面に移る。 */
  async function create() {
    // 二重に押されないよう、処理中にする。
    setBusy(true)
    try {
      // メモは、空なら送らない(サーバーは、200 バイトまで)。
      const result = await api.post<InvitationCreateResponse>("/auth/admin/invitations", { expiresInHours: Number(expiresInHours), ...(note.trim() ? { note: note.trim() } : {}) })
      // 発行の画面を閉じ、コードを見せる。
      setCreating(false)
      setNote("")
      setIssued(result)
      // 一覧を、最新にする。
      await mutate()
    } catch (createError) {
      // 失敗の理由を、知らせる。
      setToast({ message: createError instanceof Error ? createError.message : "発行できませんでした", type: "error" })
    } finally {
      // 処理中を終える。
      setBusy(false)
    }
  }

  /** 選んだ招待を取り消す(使われる前のものだけ)。 */
  async function revoke() {
    // 取り消す招待が無ければ、何もしない。
    if (!revoking) return
    // 二重に押されないよう、処理中にする。
    setBusy(true)
    try {
      await api.delete(`/auth/admin/invitations/${encodeURIComponent(revoking.invitationId)}`)
      setRevoking(null)
      setToast({ message: "招待を取り消しました", type: "success" })
      await mutate()
    } catch (revokeError) {
      setToast({ message: revokeError instanceof Error ? revokeError.message : "取り消せませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div className="row">
        <div className="grow">
          <h2 className="h2">招待</h2>
          <div className="faint" style={{ fontSize: 12.5 }}>管理者だけが発行できます。コードは、1 回だけ使えます。新しい利用者は、サインアップの「招待コード」に入れます。</div>
        </div>
        <button className="btn primary" type="button" onClick={() => setCreating(true)}>招待コードを発行</button>
      </div>
      {!data && !error && <LoadingState />}
      {error && !data && <ErrorState message="招待を取得できませんでした" onRetry={() => void mutate()} />}
      {data && data.invitations.length === 0 && <div className="mut">まだ、招待はありません。</div>}
      {data?.invitations.map((invitation) => (
        <div key={invitation.invitationId} className="row" style={{ gap: 12, borderTop: "1px solid var(--line)", paddingTop: 10 }}>
          <span className={`badge ${STATUS_VIEW[invitation.status].badge}`}><i />{STATUS_VIEW[invitation.status].label}</span>
          <div className="grow" style={{ minWidth: 0 }}>
            <div>{invitation.note || <span className="faint">(メモなし)</span>}</div>
            <div className="faint mono" style={{ fontSize: 11.5 }}>
              {invitation.invitationId} ・ 作成 {shortDateTime(invitation.createdAt)} ・ 期限 {shortDateTime(invitation.expiresAt)}
              {invitation.usedAt ? ` ・ 使用 ${shortDateTime(invitation.usedAt)}` : ""}
            </div>
          </div>
          {invitation.status === "active" && <button className="btn sm danger" type="button" onClick={() => setRevoking(invitation)}>取り消す</button>}
        </div>
      ))}
      {creating && (
        <Modal title="招待コードを発行" description="発行したコードは、この画面を閉じると、二度と見られません。" onClose={() => setCreating(false)}
          footer={<><button className="btn" type="button" onClick={() => setCreating(false)}>キャンセル</button><button className="btn primary" type="button" onClick={() => void create()} disabled={busy}>発行する</button></>}>
          <label className="lbl">有効な期間
            <select className="inp" value={expiresInHours} onChange={(event) => setExpiresInHours(event.target.value)}>
              {EXPIRY_CHOICES.map((choice) => <option key={choice.value} value={choice.value}>{choice.label}</option>)}
            </select>
          </label>
          <label className="lbl">メモ(誰に渡すか、など。省略できます)
            <input className="inp" value={note} maxLength={100} onChange={(event) => setNote(event.target.value)} placeholder="例: 田中さん向け" />
          </label>
        </Modal>
      )}
      {issued && (
        <Modal title="招待コードを発行しました" description="このコードは、今だけ見られます。閉じる前に、コピーして、相手に渡してください。" onClose={() => setIssued(null)}
          footer={<button className="btn primary" type="button" onClick={() => setIssued(null)}>閉じる</button>}>
          <div className="row"><b style={{ fontSize: 13 }}>招待コード</b><span className="grow" /><CopyButton text={issued.code} /></div>
          <div className="mono" style={{ wordBreak: "break-all", padding: 12, background: "var(--panel2)", borderRadius: 6 }}>{issued.code}</div>
          <div className="faint" style={{ fontSize: 12.5 }}>期限: {shortDateTime(issued.expiresAt)}。1 回使うと、使えなくなります。</div>
        </Modal>
      )}
      {revoking && (
        <Modal title="この招待を、取り消しますか?" description="取り消した招待のコードは、使えなくなります。元に戻せません。" onClose={() => setRevoking(null)}
          footer={<><button className="btn" type="button" onClick={() => setRevoking(null)}>キャンセル</button><button className="btn danger" type="button" onClick={() => void revoke()} disabled={busy}>取り消す</button></>}>
          <div className="mono" style={{ fontSize: 12.5 }}>{revoking.invitationId}{revoking.note ? `(${revoking.note})` : ""}</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div>
  )
}

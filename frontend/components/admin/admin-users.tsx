"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import { CopyButton } from "@/components/api-token-section"
import { Badge, ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"

type AdminUser = { userId: string; userName: string; status: string; role: string; createdAt: string; locked: boolean; lockedUntil: string | null }
type UserList = { users: AdminUser[]; nextCursor: string | null }

/** 行の列の幅(ユーザー名、役割、状態、作成日時、操作)。 */
const COLUMNS = "1.4fr 90px 150px 160px 330px"

/**
 * ユーザーの管理。一覧(新しい順。もっと見る)、停止と解除、ログインのロックの解除。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒。先頭のページだけ更新される)。
 * @param props.currentUserId いまログインしている管理者の ID(自分は、停止できない)。
 */
export function AdminUsers({ refreshInterval, currentUserId }: { refreshInterval: number; currentUserId: string }) {
  // 先頭のページ(自動更新する)と、「もっと見る」で足したページ。
  const { data, error, mutate } = useSWR<UserList>("/auth/admin/users?limit=50", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [more, setMore] = useState<AdminUser[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  // 確認中の操作、処理中か、知らせ。
  const [confirming, setConfirming] = useState<{ user: AdminUser; action: "ban" | "unban" | "reset" } | null>(null)
  // 発行した一時パスワード(閉じると、二度と見られない)。
  const [issued, setIssued] = useState<{ userName: string; password: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  // 次のページのカーソル(「もっと見る」を押す前は、先頭のページのもの)。
  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor

  /** 次のページを読み足す。 */
  async function loadMore() {
    if (!nextCursor) return
    try {
      const page = await api.get<UserList>(`/auth/admin/users?limit=50&cursor=${encodeURIComponent(nextCursor)}`)
      setMore((current) => [...current, ...page.users])
      setCursor(page.nextCursor)
    } catch (loadError) {
      setToast({ message: loadError instanceof Error ? loadError.message : "読み込めませんでした", type: "error" })
    }
  }

  /** 操作を実行して、一覧を更新する。 */
  async function run(path: string, done: string) {
    setBusy(true)
    try {
      await api.post(path)
      setConfirming(null)
      setToast({ message: done, type: "success" })
      // 足したページは、古い状態を含むので、捨てて先頭から読み直す。
      setMore([])
      setCursor(undefined)
      await mutate()
    } catch (runError) {
      setToast({ message: runError instanceof Error ? runError.message : "実行できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  /** パスワードをリセットして、一時パスワードを見せる。 */
  async function resetPassword() {
    if (!confirming) return
    setBusy(true)
    try {
      const result = await api.post<{ password: string }>(`/auth/admin/users/${encodeURIComponent(confirming.user.userId)}/password-reset`)
      setIssued({ userName: confirming.user.userName, password: result.password })
      setConfirming(null)
      setMore([])
      setCursor(undefined)
      await mutate()
    } catch (resetError) {
      setToast({ message: resetError instanceof Error ? resetError.message : "リセットできませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  const users = data ? [...data.users, ...more] : []
  const cols = { "--cols": COLUMNS } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 980 }}>
      <div className="pad" style={{ paddingBottom: 8 }}><h2 className="h2">ユーザー</h2></div>
      <div className="tr th" style={cols}><span>ユーザー名</span><span>役割</span><span>状態</span><span>作成</span><span>操作</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="ユーザーを取得できませんでした" onRetry={() => void mutate()} /></div>}
      {users.map((user) => (
        <div key={user.userId} className="tr" style={cols}>
          <span style={{ minWidth: 0 }}><b>{user.userName}</b><div className="faint mono" style={{ fontSize: 11 }}>{user.userId}</div></span>
          <span><Badge status={user.role} /></span>
          <span className="row" style={{ gap: 6, flexWrap: "wrap" }}>
            <Badge status={user.status} />
            {user.locked && <span className="badge b-warn" title={`ログインの失敗でロック中(${user.lockedUntil ? shortDateTime(user.lockedUntil) : ""} まで)`}><i />🔒 ロック中</span>}
          </span>
          <span className="mono faint">{shortDateTime(user.createdAt)}</span>
          <span className="row" style={{ gap: 6 }}>
            {user.status === "banned"
              ? <button className="btn sm" type="button" onClick={() => setConfirming({ user, action: "unban" })}>停止を解除</button>
              : <button className="btn sm danger" type="button" disabled={user.userId === currentUserId} onClick={() => setConfirming({ user, action: "ban" })}>停止</button>}
            <button className="btn sm" type="button" disabled={!user.locked} onClick={() => void run(`/auth/admin/users/${encodeURIComponent(user.userId)}/unlock`, "ログインのロックを解除しました")}>ロック解除</button>
            <button className="btn sm" type="button" onClick={() => setConfirming({ user, action: "reset" })}>パスワードのリセット</button>
          </span>
        </div>
      ))}
      {nextCursor && <div className="pad"><button className="btn" type="button" onClick={() => void loadMore()}>もっと見る</button></div>}
      {confirming && (
        <Modal title={confirming.action === "ban" ? "このユーザーを停止しますか?" : confirming.action === "unban" ? "停止を解除しますか?" : "パスワードをリセットしますか?"}
          description={confirming.action === "ban" ? "ログインできなくなり、有効なセッション(refresh token)が全て無効になります。" : confirming.action === "unban" ? "ログインできるようになります。無効にしたセッションは、戻りません。" : "新しい一時パスワードを発行します。古いパスワードと、有効なセッションは、使えなくなります。ログインの失敗のロックも解けます。一時パスワードは、1 回だけ表示されます。"}
          onClose={() => setConfirming(null)}
          footer={<><button className="btn" type="button" onClick={() => setConfirming(null)}>キャンセル</button>
            <button className={`btn ${confirming.action === "unban" ? "primary" : "danger"}`} type="button" disabled={busy}
              onClick={() => confirming.action === "reset" ? void resetPassword() : void run(`/auth/admin/users/${encodeURIComponent(confirming.user.userId)}/${confirming.action}`, confirming.action === "ban" ? "停止しました" : "停止を解除しました")}>
              {confirming.action === "ban" ? "停止する" : confirming.action === "unban" ? "解除する" : "リセットする"}
            </button></>}>
          <div className="mono" style={{ fontSize: 12.5 }}>{confirming.user.userName}({confirming.user.userId})</div>
        </Modal>
      )}
      {issued && (
        <Modal title="一時パスワードを発行しました" description={`${issued.userName} の新しいパスワードです。この画面を閉じると、二度と見られません。安全な方法で、本人に渡してください。`} onClose={() => setIssued(null)}
          footer={<button className="btn primary" type="button" onClick={() => setIssued(null)}>閉じる</button>}>
          <div className="row"><b style={{ fontSize: 13 }}>一時パスワード</b><span className="grow" /><CopyButton text={issued.password} /></div>
          <div className="mono" style={{ wordBreak: "break-all", padding: 12, background: "var(--panel2)", borderRadius: 6, fontSize: 16 }}>{issued.password}</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div></div>
  )
}

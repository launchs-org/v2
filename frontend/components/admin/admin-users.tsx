"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import { Badge, ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"

type AdminUser = { userId: string; userName: string; status: string; role: string; createdAt: string }
type UserList = { users: AdminUser[]; nextCursor: string | null }

/** 行の列の幅(ユーザー名、役割、状態、作成日時、操作)。 */
const COLUMNS = "1.4fr 90px 100px 160px 230px"

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
  const [confirming, setConfirming] = useState<{ user: AdminUser; action: "ban" | "unban" } | null>(null)
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

  const users = data ? [...data.users, ...more] : []
  const cols = { "--cols": COLUMNS } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 760 }}>
      <div className="pad" style={{ paddingBottom: 8 }}><h2 className="h2">ユーザー</h2></div>
      <div className="tr th" style={cols}><span>ユーザー名</span><span>役割</span><span>状態</span><span>作成</span><span>操作</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="ユーザーを取得できませんでした" onRetry={() => void mutate()} /></div>}
      {users.map((user) => (
        <div key={user.userId} className="tr" style={cols}>
          <span style={{ minWidth: 0 }}><b>{user.userName}</b><div className="faint mono" style={{ fontSize: 11 }}>{user.userId}</div></span>
          <span><Badge status={user.role} /></span>
          <span><Badge status={user.status} /></span>
          <span className="mono faint">{shortDateTime(user.createdAt)}</span>
          <span className="row" style={{ gap: 6 }}>
            {user.status === "banned"
              ? <button className="btn sm" type="button" onClick={() => setConfirming({ user, action: "unban" })}>停止を解除</button>
              : <button className="btn sm danger" type="button" disabled={user.userId === currentUserId} onClick={() => setConfirming({ user, action: "ban" })}>停止</button>}
            <button className="btn sm" type="button" onClick={() => void run(`/auth/admin/users/${encodeURIComponent(user.userId)}/unlock`, "ログインのロックを解除しました")}>ロック解除</button>
          </span>
        </div>
      ))}
      {nextCursor && <div className="pad"><button className="btn" type="button" onClick={() => void loadMore()}>もっと見る</button></div>}
      {confirming && (
        <Modal title={confirming.action === "ban" ? "このユーザーを停止しますか?" : "停止を解除しますか?"}
          description={confirming.action === "ban" ? "ログインできなくなり、有効なセッション(refresh token)が全て無効になります。" : "ログインできるようになります。無効にしたセッションは、戻りません。"}
          onClose={() => setConfirming(null)}
          footer={<><button className="btn" type="button" onClick={() => setConfirming(null)}>キャンセル</button>
            <button className={`btn ${confirming.action === "ban" ? "danger" : "primary"}`} type="button" disabled={busy}
              onClick={() => void run(`/auth/admin/users/${encodeURIComponent(confirming.user.userId)}/${confirming.action}`, confirming.action === "ban" ? "停止しました" : "停止を解除しました")}>
              {confirming.action === "ban" ? "停止する" : "解除する"}
            </button></>}>
          <div className="mono" style={{ fontSize: 12.5 }}>{confirming.user.userName}({confirming.user.userId})</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div></div>
  )
}

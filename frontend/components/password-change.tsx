"use client"

import { useState } from "react"
import { ApiError, changePassword } from "@/lib/api"
import type { User } from "@/lib/types"

/**
 * パスワードの変更のフォーム(現在のパスワード、新しいパスワード、確認)。
 * 変更に成功すると、新しいセッション(古いセッションは、無効になる)で、最新のユーザー情報を渡す。
 *
 * @param props.currentLabel 現在のパスワードの欄の名前(一時パスワードのときは、「一時パスワード」)。
 * @param props.onChanged 変更できたとき(最新のユーザー情報を受け取る)。
 */
export function PasswordChangeForm({ currentLabel = "現在のパスワード", onChanged }: { currentLabel?: string; onChanged: (user: User) => void }) {
  // 入力の値と、処理中、エラー。
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  /** 入力を確かめて、変更する。 */
  async function submit(event: React.FormEvent) {
    event.preventDefault()
    // 画面で分かる間違いは、送る前に伝える。
    if (next !== confirmation) {
      setError("新しいパスワードが、確認の入力と一致しません")
      return
    }
    setBusy(true)
    setError("")
    try {
      onChanged(await changePassword(current, next))
    } catch (changeError) {
      setError(changeError instanceof ApiError ? changeError.message : "変更できませんでした。接続を確認してください")
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={(event) => void submit(event)} style={{ display: "flex", flexDirection: "column", gap: 14, maxWidth: 420 }}>
      <label className="lbl">{currentLabel}<input className="inp" type="password" autoComplete="current-password" value={current} onChange={(event) => setCurrent(event.target.value)} required /></label>
      <label className="lbl">新しいパスワード<input className="inp" type="password" autoComplete="new-password" value={next} onChange={(event) => setNext(event.target.value)} placeholder="12文字以上" required /></label>
      <label className="lbl">新しいパスワード(確認)<input className="inp" type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required /></label>
      {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>{error}</div>}
      <div><button className="btn primary" type="submit" disabled={busy || !current || !next || !confirmation}>{busy ? "変更しています…" : "パスワードを変更"}</button></div>
    </form>
  )
}

/**
 * 一時パスワードでログインしたとき、他の画面より先に出す、パスワードの変更の画面。変更するか、ログアウトするまで、先へ進めない。
 *
 * @param props.user ログインしているユーザー。
 * @param props.onChanged 変更できたとき。
 * @param props.onLogout ログアウトするとき。
 */
export function ForcedPasswordScreen({ user, onChanged, onLogout }: { user: User; onChanged: (user: User) => void; onLogout: () => Promise<void> }) {
  return (
    <div className="app app-loading" style={{ padding: 16 }}>
      <div className="card pad" style={{ width: "min(480px, 100%)", display: "flex", flexDirection: "column", gap: 16 }}>
        <div>
          <h1 className="h1 disp" style={{ fontSize: 24 }}>パスワードを変更してください</h1>
          <div className="faint" style={{ fontSize: 13, marginTop: 6 }}>{user.userName} さんは、管理者が発行した一時パスワードでログインしています。自分だけが知るパスワードに変えるまで、先へ進めません。</div>
        </div>
        <PasswordChangeForm currentLabel="一時パスワード" onChanged={onChanged} />
        <div><button className="btn ghost" type="button" onClick={() => void onLogout()}>ログアウト</button></div>
      </div>
    </div>
  )
}

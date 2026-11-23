"use client"

import { useEffect, useState } from "react"
import { AdminAccessLogs } from "@/components/admin/admin-access-logs"
import { AdminDeploys } from "@/components/admin/admin-deploys"
import { AdminQuota } from "@/components/admin/admin-quota"
import { AdminSecurity } from "@/components/admin/admin-security"
import { AdminSettings } from "@/components/admin/admin-settings"
import { AdminLoginEvents } from "@/components/admin/admin-login-events"
import { AdminOverview } from "@/components/admin/admin-overview"
import { AdminUsers } from "@/components/admin/admin-users"
import { InvitationSection } from "@/components/invitation-section"
import { LoginScreen, type LoginInput } from "@/components/login-screen"
import { ForcedPasswordScreen } from "@/components/password-change"
import { ApiError, clearSessionTokens, restoreSession, signIn, signOut } from "@/lib/api"
import type { User } from "@/lib/types"

/** 管理画面のタブ。 */
const TABS = [
  { key: "overview", label: "概要" },
  { key: "users", label: "ユーザー" },
  { key: "invitations", label: "招待" },
  { key: "logins", label: "ログイン履歴" },
  { key: "deploys", label: "デプロイ" },
  { key: "access", label: "アクセスログ" },
  { key: "quota", label: "クォータ" },
  { key: "security", label: "セキュリティ" },
  { key: "settings", label: "設定" },
] as const

type TabKey = (typeof TABS)[number]["key"]

/** 自動更新の間隔(ミリ秒)。 */
const REFRESH_INTERVAL = 15_000

/**
 * 管理画面(/admin)。管理者のアカウント(最初に作った bootstrap の管理者など)でログインしたときだけ、中が見える。
 * 管理者でないアカウントでログインすると、その旨を出して、ログアウトさせる(サーバーも、管理 API を 403 にする)。
 */
export function AdminApp() {
  // ログインしているユーザーと、ログインの状態。
  const [user, setUser] = useState<User | null>(null)
  const [authReady, setAuthReady] = useState(false)
  const [authenticating, setAuthenticating] = useState(false)
  const [authError, setAuthError] = useState("")
  // 開いているタブ。
  const [tab, setTab] = useState<TabKey>("overview")

  // タブは、URL のハッシュ(#users など)に持つ(再読み込みや、戻るボタン、リンクでも、同じタブが開く)。
  useEffect(() => {
    const readHash = () => {
      const key = window.location.hash.replace(/^#/, "")
      setTab(TABS.some((item) => item.key === key) ? (key as TabKey) : "overview")
    }
    readHash()
    window.addEventListener("hashchange", readHash)
    return () => window.removeEventListener("hashchange", readHash)
  }, [])

  // 保存してある refresh token で、ログインの状態を戻す。期限切れの通知も受ける。
  useEffect(() => {
    const onExpired = () => {
      clearSessionTokens()
      setUser(null)
      setAuthError("セッションの有効期限が切れました。もう一度ログインしてください。")
    }
    window.addEventListener("launchs:session-expired", onExpired)
    let cancelled = false
    void restoreSession().then((restored) => {
      if (cancelled) return
      if (restored) setUser(restored)
      setAuthReady(true)
    })
    return () => {
      cancelled = true
      window.removeEventListener("launchs:session-expired", onExpired)
    }
  }, [])

  /** ログインする(サインアップは、管理画面では受け付けない)。 */
  async function handleSubmit(input: LoginInput) {
    setAuthenticating(true)
    setAuthError("")
    try {
      setUser(await signIn(input.userName, input.password, input.turnstileToken))
    } catch (error) {
      setAuthError(error instanceof ApiError ? error.message : "接続に失敗しました。APIの稼働状態をご確認ください。")
    } finally {
      setAuthenticating(false)
    }
  }

  /** ログアウトする。 */
  async function handleLogout() {
    await signOut().catch(() => undefined)
    setUser(null)
  }

  if (!authReady) return <div className="app app-loading"><span className="loader" /></div>
  if (!user) return <LoginScreen onSubmit={handleSubmit} loading={authenticating} error={authError} adminOnly />
  // 一時パスワードのままなら(コマンドで置き換えた直後など)、先にパスワードを変えてもらう。
  if (user.mustChangePassword) return <ForcedPasswordScreen user={user} onChanged={setUser} onLogout={handleLogout} />
  // 管理者でなければ、中を見せない。
  if (user.role !== "admin") {
    return (
      <div className="app app-loading" style={{ flexDirection: "column", gap: 14 }}>
        <div>このアカウントは、管理者ではありません。</div>
        <button className="btn" type="button" onClick={() => void handleLogout()}>ログアウト</button>
      </div>
    )
  }

  return (
    <div className="app">
      <div className="shell">
        <header className="topnav">
          <span className="disp" style={{ fontSize: 18, fontWeight: 500 }}>Launchs 管理</span>
          <div className="tabs" role="tablist" style={{ marginLeft: 8 }}>
            {TABS.map((item) => <button key={item.key} type="button" role="tab" aria-selected={tab === item.key} className={`tab ${tab === item.key ? "on" : ""}`} onClick={() => { window.location.hash = item.key }}>{item.label}</button>)}
          </div>
          <div className="grow" />
          <span className="faint" style={{ fontSize: 12.5 }}>{user.userName}</span>
          <a className="btn sm ghost" href="/">サービスへ</a>
          <button className="btn sm" type="button" onClick={() => void handleLogout()}>ログアウト</button>
        </header>
        <div className="stage">
          <div className="page">
            {tab === "overview" && <AdminOverview refreshInterval={REFRESH_INTERVAL} />}
            {tab === "users" && <AdminUsers refreshInterval={REFRESH_INTERVAL} currentUserId={user.userId} />}
            {tab === "invitations" && <InvitationSection refreshInterval={REFRESH_INTERVAL} />}
            {tab === "deploys" && <AdminDeploys refreshInterval={REFRESH_INTERVAL} />}
            {tab === "access" && <AdminAccessLogs refreshInterval={REFRESH_INTERVAL} />}
            {tab === "quota" && <AdminQuota />}
            {tab === "security" && <AdminSecurity />}
            {tab === "settings" && <AdminSettings refreshInterval={REFRESH_INTERVAL} />}
            {tab === "logins" && <AdminLoginEvents refreshInterval={REFRESH_INTERVAL} />}
          </div>
        </div>
      </div>
    </div>
  )
}

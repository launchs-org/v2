"use client"

import { useEffect, useState } from "react"
import { ConsoleShell } from "@/components/console-shell"
import { LoginScreen } from "@/components/login-screen"
import { ApiError, clearSessionTokens, signIn, signOut } from "@/lib/api"
import type { User } from "@/lib/types"

export function ConsoleApp() {
  const [user, setUser] = useState<User | null>(null)
  const [authenticating, setAuthenticating] = useState(false)
  const [authError, setAuthError] = useState("")
  const [authReady, setAuthReady] = useState(false)

  useEffect(() => {
    const onExpired = () => {
      clearSessionTokens()
      setUser(null)
      setAuthError("セッションの有効期限が切れました。もう一度ログインしてください。")
    }
    window.addEventListener("launchs:session-expired", onExpired)
    setAuthReady(true)
    return () => window.removeEventListener("launchs:session-expired", onExpired)
  }, [])

  async function handleLogin(userName: string, password: string, turnstileToken?: string) {
    setAuthenticating(true)
    setAuthError("")
    try {
      const nextUser = await signIn(userName, password, turnstileToken)
      setUser(nextUser)
    } catch (error) {
      setAuthError(error instanceof ApiError ? error.message : "接続に失敗しました。APIの稼働状態をご確認ください。")
    } finally {
      setAuthenticating(false)
    }
  }

  async function handleLogout() {
    await signOut().catch(() => undefined)
    setUser(null)
  }

  if (!authReady) return <div className="app-loading"><span className="loader" /></div>
  if (!user) return <LoginScreen onLogin={handleLogin} loading={authenticating} error={authError} />
  return <ConsoleShell user={user} onLogout={handleLogout} />
}

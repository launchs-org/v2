"use client"

import { useRef, useState } from "react"
import Script from "next/script"
import useSWR from "swr"
import { publicFetcher } from "@/lib/api"
import type { AuthConfig } from "@/lib/types"

declare global {
  interface Window {
    turnstile?: {
      render: (container: HTMLElement, options: { sitekey: string; callback: (token: string) => void; "expired-callback"?: () => void }) => string
      reset: (widgetId?: string) => void
    }
  }
}

/** ログインとサインアップの、入力。 */
export type LoginInput = { mode: "login" | "signup"; userName: string; password: string; invitationCode?: string; turnstileToken?: string }

type LoginScreenProps = {
  onSubmit: (input: LoginInput) => Promise<void>
  loading: boolean
  error: string
  /** true なら、サインアップを出さない(管理画面のログイン用)。 */
  adminOnly?: boolean
}

/** 左の、飾りのコード(モックと同じ)。 */
const CODE_BG = `// app.js
import express from "express";
const app = express();
app.get("/", (req, res) => {
  res.send("Hello, Launchs!");
});
app.listen(8080);

# main.py
from flask import Flask
app = Flask(__name__)`

/**
 * ログインとサインアップの画面(左に飾り、右にフォーム。モックのデザイン)。
 * 人間であることの確認(Turnstile)が有効なときは、ウィジェットを出す。
 *
 * @param props.onSubmit 送信されたとき(ログインかサインアップかを含む)。
 * @param props.loading 送信中か。
 * @param props.error 直前の失敗のメッセージ。
 */
export function LoginScreen({ onSubmit, loading, error, adminOnly = false }: LoginScreenProps) {
  const { data: authConfig } = useSWR<AuthConfig>("/auth/config", publicFetcher, { refreshInterval: 60_000, revalidateOnFocus: false })
  const [mode, setMode] = useState<"login" | "signup">("login")
  const [userName, setUserName] = useState("")
  const [password, setPassword] = useState("")
  const [invitationCode, setInvitationCode] = useState("")
  const [turnstileToken, setTurnstileToken] = useState("")
  const captchaNode = useRef<HTMLDivElement>(null)
  const captchaId = useRef<string | undefined>(undefined)
  const turnstileRequired = Boolean(authConfig?.turnstile?.enabled)
  const siteKey = authConfig?.turnstile?.siteKey
  const turnstileEnabled = Boolean(turnstileRequired && siteKey)
  const challengeUnavailable = turnstileRequired && !siteKey

  /** Turnstile のウィジェットを、1度だけ、描く。 */
  function mountTurnstile() {
    if (!siteKey || !captchaNode.current || !window.turnstile || captchaId.current) return
    captchaId.current = window.turnstile.render(captchaNode.current, { sitekey: siteKey, callback: setTurnstileToken, "expired-callback": () => setTurnstileToken("") })
  }

  /**
   * フォームが送信されたとき: 送ってから、Turnstile を取り直す(トークンは、1回しか使えない)。
   *
   * @param event フォームの送信イベント。
   */
  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (turnstileEnabled && !turnstileToken) return
    await onSubmit({ mode, userName: userName.trim(), password, invitationCode: invitationCode.trim() || undefined, turnstileToken: turnstileToken || undefined })
    if (turnstileEnabled) {
      setTurnstileToken("")
      if (captchaId.current) window.turnstile?.reset(captchaId.current)
    }
  }

  return (
    <div className="app">
      {turnstileEnabled && <Script src="https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit" strategy="afterInteractive" onLoad={mountTurnstile} />}
      <div className="login">
        <div className="lhero" style={{ position: "relative", overflow: "hidden" }}>
          <pre className="codebg" aria-hidden="true">{CODE_BG}</pre>
          <div className="row" style={{ gap: 12 }}>
            <svg viewBox="0 0 32 32" width="34" height="34" aria-hidden="true"><rect width="32" height="32" rx="9" fill="var(--accent)" /><path d="M9 22l7-14 7 14M12.5 17h7" stroke="#fff" strokeWidth="2.4" fill="none" strokeLinecap="round" strokeLinejoin="round" /></svg>
            <span className="disp" style={{ fontSize: 22, fontWeight: 500 }}>Launchs</span>
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <h1 className="disp" style={{ margin: 0, fontSize: "clamp(32px, 9vw, 56px)", lineHeight: 1.2, fontWeight: 500, letterSpacing: "-.02em" }}><span style={{ display: "block" }}>デプロイを、</span><span style={{ display: "block", color: "var(--accent)" }}>もっと簡単に。</span></h1>
            <p style={{ margin: 0, fontSize: 20 }}>インフラの心配を、減らす。</p>
          </div>
        </div>
        <div className="lform">
          <form style={{ width: "min(400px,100%)", display: "flex", flexDirection: "column", gap: 18 }} onSubmit={handleSubmit}>
            {adminOnly && <div><div className="disp" style={{ fontSize: 22 }}>管理画面</div><div className="faint" style={{ fontSize: 12.5 }}>管理者のアカウントでログインしてください。</div></div>}
            {!adminOnly && <div className="seg" role="tablist">
              <button type="button" role="tab" aria-selected={mode === "login"} className={mode === "login" ? "on" : ""} onClick={() => setMode("login")}>ログイン</button>
              <button type="button" role="tab" aria-selected={mode === "signup"} className={mode === "signup" ? "on" : ""} onClick={() => setMode("signup")}>サインアップ</button>
            </div>}
            <label className="lbl">ユーザー名<input className="inp" type="text" autoComplete="username" autoCapitalize="none" value={userName} onChange={(event) => setUserName(event.target.value)} placeholder="英数字とハイフン 1〜64文字" required /></label>
            <label className="lbl">パスワード<input className="inp" type="password" autoComplete={mode === "login" ? "current-password" : "new-password"} value={password} onChange={(event) => setPassword(event.target.value)} placeholder="12文字以上" required /></label>
            {mode === "signup" && <label className="lbl">招待コード<input className="inp mono" type="text" value={invitationCode} onChange={(event) => setInvitationCode(event.target.value)} placeholder="招待制のときに必須" /></label>}
            {turnstileEnabled && <div ref={captchaNode} />}
            {challengeUnavailable && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>人間であることの確認の設定を取得できません。管理者にご確認ください。</div>}
            {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>{error}</div>}
            <button className="btn primary" type="submit" style={{ minHeight: 46 }} disabled={loading || challengeUnavailable || (turnstileEnabled && !turnstileToken)}>
              {loading ? "処理しています…" : mode === "login" ? "ログイン" : "アカウントを作成"}
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}

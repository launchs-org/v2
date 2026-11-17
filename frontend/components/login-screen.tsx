"use client"

import { useRef, useState } from "react"
import Script from "next/script"
import useSWR from "swr"
import { ArrowRight, Eye, EyeOff, LockKeyhole, ShieldCheck, TerminalSquare } from "lucide-react"
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

type LoginScreenProps = {
  onLogin: (userName: string, password: string, turnstileToken?: string) => Promise<void>
  loading: boolean
  error: string
}

export function LoginScreen({ onLogin, loading, error }: LoginScreenProps) {
  const { data: authConfig } = useSWR<AuthConfig>("/auth/config", publicFetcher, { refreshInterval: 60_000, revalidateOnFocus: false })
  const [userName, setUserName] = useState("")
  const [password, setPassword] = useState("")
  const [turnstileToken, setTurnstileToken] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const captchaNode = useRef<HTMLDivElement>(null)
  const captchaId = useRef<string | undefined>(undefined)
  const turnstileRequired = Boolean(authConfig?.turnstile?.enabled)
  const turnstileEnabled = Boolean(turnstileRequired && authConfig?.turnstile.siteKey)
  const siteKey = authConfig?.turnstile?.siteKey
  const challengeUnavailable = turnstileRequired && !siteKey

  function mountTurnstile() {
    if (!siteKey || !captchaNode.current || !window.turnstile || captchaId.current) return
    captchaId.current = window.turnstile.render(captchaNode.current, {
      sitekey: siteKey,
      callback: setTurnstileToken,
      "expired-callback": () => setTurnstileToken(""),
    })
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (turnstileEnabled && !turnstileToken) return
    await onLogin(userName.trim(), password, turnstileToken || undefined)
    if (turnstileEnabled) {
      setTurnstileToken("")
      if (captchaId.current) window.turnstile?.reset(captchaId.current)
    }
  }

  return (
    <main className="login-screen">
      {turnstileEnabled && <Script src="https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit" strategy="afterInteractive" onLoad={mountTurnstile} />}
      <section className="login-visual" aria-label="Launchs コンテナプラットフォーム">
        <div className="brand-lockup"><span className="brand-mark"><TerminalSquare size={18} strokeWidth={2.2} /></span><span>launchs</span></div>
        <div className="login-copy">
          <div className="login-eyebrow">Container control plane</div>
          <h1>Ship faster.<br />Stay in control.</h1>
          <p>コンテナ、デプロイ、ネットワーク、ストレージ。インフラ運用に必要なすべてを、ひとつの静かなコントロールルームに。</p>
        </div>
        <div className="login-terminal" aria-hidden="true">
          <div className="terminal-head"><span className="terminal-dot" /><span className="terminal-dot" /><span className="terminal-dot" /><span className="terminal-label">LAUNCHS / CONTROL PLANE</span></div>
          <div className="terminal-line"><span className="prompt">$</span><span>launchs inspect --project &lt;id&gt;</span></div>
          <div className="terminal-line"><span className="prompt">›</span><span>pending → apply → observed</span></div>
        </div>
        <div className="login-footer">SECURE WORKSPACE · OBSERVE · APPLY</div>
      </section>
      <section className="login-panel">
        <div className="login-form-wrap">
          <div className="login-mobile-brand"><span className="brand-mark"><TerminalSquare size={16} /></span><span>launchs</span></div>
          <h2 className="login-title">おかえりなさい</h2>
          <p className="login-subtitle">Launchs コントロールプレーンにログインしてください。</p>
          <form onSubmit={handleSubmit}>
            <div className="form-field">
              <label className="form-label" htmlFor="username">ユーザー名</label>
              <input id="username" className="form-control" autoComplete="username" autoCapitalize="none" value={userName} onChange={(event) => setUserName(event.target.value)} placeholder="ユーザー名を入力" required />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="password">パスワード</label>
              <div className="password-wrap">
                <input id="password" className="form-control" type={showPassword ? "text" : "password"} autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="パスワードを入力" required />
                <button className="icon-button password-toggle" type="button" onClick={() => setShowPassword((value) => !value)} aria-label={showPassword ? "パスワードを隠す" : "パスワードを表示"}>{showPassword ? <EyeOff size={16} /> : <Eye size={16} />}</button>
              </div>
            </div>
            {turnstileEnabled && <div className="turnstile-slot"><div ref={captchaNode} /><p className="editor-help">安全確認を完了するとログインできます。</p></div>}
            {challengeUnavailable && <div className="error-banner" role="alert"><ShieldCheck size={15} /><span>API 側で Turnstile が有効ですが、公開 site key を取得できません。管理者に設定をご確認ください。</span></div>}
            {error && <div className="error-banner" role="alert"><ShieldCheck size={15} /><span>{error}</span></div>}
            <button className="btn btn-primary btn-block" type="submit" disabled={loading || challengeUnavailable || (turnstileEnabled && !turnstileToken)}>
              {loading ? <><span className="loader" />認証しています</> : <>ログイン<ArrowRight size={15} /></>}
            </button>
          </form>
          <div className="login-note"><LockKeyhole size={13} style={{ verticalAlign: "-2px", marginRight: 5 }} />デモモードです。任意のユーザー名とパスワードでログインできます。データはこのプレビュー内のメモリだけで管理されます。</div>
        </div>
      </section>
    </main>
  )
}

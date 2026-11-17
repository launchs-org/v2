"use client"

import useSWR from "swr"
import { Clock3, Fingerprint, KeyRound, LogOut, ShieldCheck, UserRound } from "lucide-react"
import { authenticatedFetcher } from "@/lib/api"
import type { LoginHistoryResponse, User } from "@/lib/types"
import { ErrorState, formattedDate, LoadingState, PageHeading, StatusPill } from "@/components/ui-kit"

export function AccountView({ user, refreshInterval, onLogout }: { user: User; refreshInterval: number; onLogout: () => Promise<void> }) {
  const { data, error, mutate } = useSWR<LoginHistoryResponse>("/auth/login-history?limit=20", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  return <section>
    <PageHeading eyebrow="ACCOUNT & SECURITY" title="アカウント設定" description="ログイン中のユーザー情報と最近の認証アクティビティを確認します。" />
    <div className="account-grid">
      <div className="account-main-column">
        <div className="card account-profile-card"><div className="account-profile-head"><span className="account-avatar-large"><UserRound size={22} /></span><div><span className="small-label">SIGNED IN AS</span><h2>{user.userName}</h2><div className="account-profile-badges"><StatusPill status={user.role} /><StatusPill status={user.status} /></div></div></div><div className="account-fields"><div className="account-field"><span>ユーザー ID</span><code>{user.userId}</code></div><div className="account-field"><span>アカウント作成日</span><strong>{formattedDate(user.createdAt)}</strong></div><div className="account-field"><span>権限</span><strong>{user.role === "admin" ? "Administrator" : "Workspace member"}</strong></div><div className="account-field"><span>アカウント状態</span><strong>{user.status === "active" ? "有効" : "停止中"}</strong></div></div></div>
        <div className="card login-history-card"><div className="card-head"><div><h2 className="card-title">最近のログイン</h2><p className="card-subtitle">アカウントの認証履歴 · 最大20件</p></div><button className="icon-button" type="button" onClick={() => void mutate()} title="履歴を更新" aria-label="ログイン履歴を更新"><Clock3 size={15} /></button></div>{error && <ErrorState message="ログイン履歴を取得できませんでした。" onRetry={() => void mutate()} />}{!data ? <div className="card-body"><LoadingState label="ログイン履歴を取得しています" /></div> : data.history.length === 0 ? <div className="empty-state"><div className="empty-icon"><Clock3 size={17} /></div><strong>ログイン履歴はありません</strong><p>新しい認証履歴がここに表示されます。</p></div> : <div className="login-history-list">{data.history.map((entry, index) => <div className="login-history-row" key={entry.historyId}><span className={`history-marker ${index === 0 ? "current" : ""}`}><ShieldCheck size={14} /></span><div className="history-info"><strong>{index === 0 ? "現在のセッション" : "ログイン"}</strong><span>{entry.ip} · {entry.userAgent}</span></div><time dateTime={entry.createdAt}>{formattedDate(entry.createdAt)}</time></div>)}</div>}</div>
      </div>
      <aside className="account-side-column"><div className="card security-card"><div className="security-icon"><ShieldCheck size={18} /></div><h3>デモモードで動作中</h3><p>この画面はモックデータを使用しています。変更内容はブラウザーのメモリ上だけに保持され、再読み込みで初期状態に戻ります。</p><div className="security-row"><KeyRound size={14} /><span>ローカル認証</span><span className="security-ok">デモ</span></div><div className="security-row"><Fingerprint size={14} /><span>ログイン履歴</span><span className="security-ok">サンプル</span></div></div><div className="card session-card"><div><h3>このデバイスからログアウト</h3><p>このブラウザーの認証セッションを終了します。</p></div><button className="btn btn-danger" type="button" onClick={() => void onLogout()}><LogOut size={14} />ログアウト</button></div><div className="info-banner account-api-note">アカウント情報とログイン履歴は UI 確認用のサンプルです。実際の認証サーバーには接続していません。</div></aside>
    </div>
  </section>
}

"use client"

import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import type { LoginEventsResponse, User } from "@/lib/types"
import { ApiTokenSection } from "@/components/api-token-section"
import { Badge, ErrorState, LoadingState } from "@/components/ui-kit"

/** 失敗の理由を、画面に出す言葉にする。 */
const REASON_LABEL: Record<string, string> = { wrong_password: "パスワードの誤り", locked: "ロック中", banned: "停止中" }

/**
 * アカウント設定の画面(ユーザー情報、ログイン履歴、ログアウト)。
 *
 * @param props.user ログインしているユーザー。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 * @param props.onLogout ログアウトするとき。
 */
export function AccountView({ user, refreshInterval, onLogout }: { user: User; refreshInterval: number; onLogout: () => Promise<void> }) {
  const { data, error, mutate } = useSWR<LoginEventsResponse>("/auth/login-history?limit=20", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  return (
    <div className="ws" style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <h1 className="h1 disp">アカウント設定</h1>
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 16 }}>
        <div className="row"><h2 className="h2 grow">プロフィール</h2><button className="btn" type="button" onClick={() => void onLogout()}>ログアウト</button></div>
        <dl className="kv" style={{ margin: 0 }}>
          <div><dt>ユーザー名</dt><dd>{user.userName}</dd></div>
          <div><dt>役割</dt><dd><Badge status={user.role} /></dd></div>
          <div><dt>ユーザー ID</dt><dd className="mono">{user.userId}</dd></div>
        </dl>
      </div>
      <ApiTokenSection refreshInterval={refreshInterval} />
      <div className="card tscroll"><div className="tinner">
        <div className="pad" style={{ paddingBottom: 8 }}><h2 className="h2">最近のログイン</h2></div>
        <div className="tr th" style={{ "--cols": "170px 110px 110px 1fr 1.6fr" } as React.CSSProperties}><span>日時</span><span>種類</span><span>結果</span><span>IP</span><span>ブラウザ</span></div>
        {!data && !error && <LoadingState />}
        {error && !data && <div className="pad"><ErrorState message="履歴を取得できませんでした。" onRetry={() => void mutate()} /></div>}
        {data?.events.map((event, index) => (
          <div key={`${event.at}-${index}`} className="tr" style={{ "--cols": "170px 110px 110px 1fr 1.6fr" } as React.CSSProperties}>
            <span className="mono faint">{shortDateTime(event.at)}</span>
            <span>{event.kind === "signup" ? "サインアップ" : "ログイン"}</span>
            <span><span className={`badge ${event.result === "success" ? "b-ok" : "b-bad"}`}><i />{event.result === "success" ? "成功" : REASON_LABEL[event.reason ?? ""] ?? "失敗"}</span></span>
            <span className="mono">{event.ip}</span>
            <span className="mono mut" style={{ fontSize: 11.5, wordBreak: "break-all" }}>{event.userAgent}</span>
          </div>
        ))}
        {data && !data.events.length && <div className="tr mut" style={{ "--cols": "1fr" } as React.CSSProperties}>履歴はありません。</div>}
      </div></div>
    </div>
  )
}

"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import { ErrorState, LoadingState } from "@/components/ui-kit"

type LoginEvent = { eventId: number; userId: string | null; userName: string; kind: "signup" | "login"; result: "success" | "failure"; reason: string | null; ip: string; userAgent: string; occurredAt: string }
type LoginEventList = { events: LoginEvent[]; nextCursor: string | null }

/** 失敗の理由を、画面の言葉にする。 */
const REASON_LABEL: Record<string, string> = {
  unknown_user: "存在しないユーザー名",
  wrong_password: "パスワードの間違い",
  locked: "ロック中",
  banned: "停止中のユーザー",
  invalid_input: "入力の不正",
  blocked: "ブロックされた IP",
}

/** 行の列の幅(日時、ユーザー名、種類、結果、理由、IP、ブラウザ)。 */
const COLUMNS = "150px 1fr 100px 80px 160px 140px 1.4fr"

/**
 * 全ユーザーのログイン履歴(失敗も含む。存在しない名前への試行も出る)。不正なアクセスの確認に使う。
 * 結果と、ユーザー名の一部で絞り込める。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function AdminLoginEvents({ refreshInterval }: { refreshInterval: number }) {
  // 絞り込み(結果と、ユーザー名の一部)。
  const [result, setResult] = useState("")
  const [keyword, setKeyword] = useState("")
  const query = `limit=100${result ? `&result=${result}` : ""}${keyword.trim() ? `&userName=${encodeURIComponent(keyword.trim())}` : ""}`
  const { data, error, mutate } = useSWR<LoginEventList>(`/auth/admin/login-events?${query}`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  // 「もっと見る」で足したページ。
  const [more, setMore] = useState<LoginEvent[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor

  /** 絞り込みを変えたとき、足したページを捨てる。 */
  function resetPaging() {
    setMore([])
    setCursor(undefined)
  }

  /** 次のページを読み足す。 */
  async function loadMore() {
    if (!nextCursor) return
    const page = await api.get<LoginEventList>(`/auth/admin/login-events?${query}&cursor=${encodeURIComponent(nextCursor)}`)
    setMore((current) => [...current, ...page.events])
    setCursor(page.nextCursor)
  }

  const events = data ? [...data.events, ...more] : []
  const cols = { "--cols": COLUMNS } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 980 }}>
      <div className="pad row" style={{ paddingBottom: 8, gap: 10, flexWrap: "wrap" }}>
        <h2 className="h2 grow">ログイン履歴</h2>
        <select className="inp" style={{ width: "auto" }} value={result} onChange={(event) => { setResult(event.target.value); resetPaging() }} aria-label="結果で絞り込む">
          <option value="">すべての結果</option><option value="failure">失敗だけ</option><option value="success">成功だけ</option>
        </select>
        <input className="inp" style={{ width: 200 }} value={keyword} onChange={(event) => { setKeyword(event.target.value); resetPaging() }} placeholder="ユーザー名で絞り込む" aria-label="ユーザー名で絞り込む" />
        <button className="btn" type="button" onClick={() => { resetPaging(); void mutate() }}>更新</button>
      </div>
      <div className="tr th" style={cols}><span>日時</span><span>ユーザー名</span><span>種類</span><span>結果</span><span>失敗の理由</span><span>IP</span><span>ブラウザ</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="履歴を取得できませんでした" onRetry={() => void mutate()} /></div>}
      {data && events.length === 0 && <div className="pad mut">履歴はありません。</div>}
      {events.map((event) => (
        <div key={event.eventId} className="tr" style={cols}>
          <span className="mono faint">{shortDateTime(event.occurredAt)}</span>
          <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>{event.userName}{event.userId === null && <span className="faint">(未登録)</span>}</span>
          <span>{event.kind === "signup" ? "サインアップ" : "ログイン"}</span>
          <span className={`badge ${event.result === "success" ? "b-ok" : "b-bad"}`}><i />{event.result === "success" ? "成功" : "失敗"}</span>
          <span>{event.reason ? REASON_LABEL[event.reason] ?? event.reason : ""}</span>
          <span className="mono">{event.ip}</span>
          <span className="faint" style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={event.userAgent}>{event.userAgent}</span>
        </div>
      ))}
      {nextCursor && <div className="pad"><button className="btn" type="button" onClick={() => void loadMore()}>もっと見る</button></div>}
    </div></div>
  )
}

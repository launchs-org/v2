"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import { ErrorState, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"

type SuspiciousIp = { ip: string; failures: number; successes: number; userNames: number; lastSeen: string; blocked: boolean }
type Block = { blockId: string; network: string; reason: string; createdByName: string; createdBy: string; createdAt: string; expiresAt: string | null }
type AuditLog = { time: string; actorId: string; actorName: string; action: string; target: string; detail: string; ip: string }
type AuditList = { logs: AuditLog[]; nextCursor: string | null }

/** 操作の名前を、画面の言葉にする。 */
const ACTION_LABEL: Record<string, string> = {
  ban: "ユーザーの停止", unban: "停止の解除", unlock: "ロックの解除", invitation_create: "招待の発行", invitation_revoke: "招待の取り消し",
  admin_create: "管理者の作成", admin_promote: "管理者への昇格", ip_block: "IP のブロック", ip_unblock: "ブロックの解除", quota_defaults_update: "全体のクォータの変更",
}

/** 期限の選択肢(時間。0 は、解除するまで)。 */
const EXPIRY_CHOICES = [{ value: "0", label: "解除するまで" }, { value: "1", label: "1 時間" }, { value: "24", label: "1 日" }, { value: "168", label: "7 日" }, { value: "720", label: "30 日" }]

/** 不審な IP の一覧と、ブロックの操作。 */
function SuspiciousSection({ onBlock }: { onBlock: (network: string) => void }) {
  const [hours, setHours] = useState("24")
  const [minFailures, setMinFailures] = useState("5")
  const { data, error, mutate } = useSWR<{ ips: SuspiciousIp[] }>(`/admin-api/security/suspicious-ips?hours=${hours}&minFailures=${minFailures}`, authenticatedFetcher, { refreshInterval: 15_000 })
  const cols = { "--cols": "1.2fr 90px 90px 110px 160px 110px" } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 780 }}>
      <div className="pad row" style={{ paddingBottom: 8, gap: 10, flexWrap: "wrap" }}>
        <div className="grow"><h2 className="h2">不審な IP</h2><div className="faint" style={{ fontSize: 12.5 }}>ログインの失敗が多い IP です。ユーザー名の種類が多いほど、総当たりの疑いが強いです。</div></div>
        <select className="inp" style={{ width: "auto" }} value={hours} onChange={(event) => setHours(event.target.value)} aria-label="期間"><option value="1">1 時間</option><option value="24">24 時間</option><option value="168">7 日</option><option value="720">30 日</option></select>
        <select className="inp" style={{ width: "auto" }} value={minFailures} onChange={(event) => setMinFailures(event.target.value)} aria-label="失敗の数"><option value="3">失敗 3 回以上</option><option value="5">失敗 5 回以上</option><option value="20">失敗 20 回以上</option><option value="100">失敗 100 回以上</option></select>
      </div>
      <div className="tr th" style={cols}><span>IP</span><span>失敗</span><span>成功</span><span>ユーザー名の種類</span><span>最後の試行</span><span>操作</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="取得できませんでした" onRetry={() => void mutate()} /></div>}
      {data && data.ips.length === 0 && <div className="pad mut">該当する IP はありません。</div>}
      {data?.ips.map((item) => (
        <div key={item.ip} className="tr" style={cols}>
          <span className="mono">{item.ip}</span><span><b>{item.failures}</b></span><span>{item.successes}</span><span>{item.userNames}</span>
          <span className="mono faint">{shortDateTime(item.lastSeen)}</span>
          <span>{item.blocked ? <span className="badge b-bad"><i />ブロック中</span> : <button className="btn sm danger" type="button" onClick={() => onBlock(item.ip)}>ブロック</button>}</span>
        </div>
      ))}
    </div></div>
  )
}

/** ブロック中の一覧と、解除。 */
function BlockedSection({ onAdd, refreshKey, onChanged }: { onAdd: () => void; refreshKey: number; onChanged: (message: string, type: "success" | "error") => void }) {
  const { data, error, mutate } = useSWR<{ blocks: Block[] }>(["/admin-api/security/blocked-ips", refreshKey], ([path]: [string, number]) => authenticatedFetcher<{ blocks: Block[] }>(path))
  const [unblocking, setUnblocking] = useState<Block | null>(null)
  const [busy, setBusy] = useState(false)
  const cols = { "--cols": "1.2fr 1.4fr 140px 160px 160px 100px" } as React.CSSProperties

  /** ブロックを解除する。 */
  async function unblock() {
    if (!unblocking) return
    setBusy(true)
    try {
      await api.delete(`/admin-api/security/blocked-ips/${encodeURIComponent(unblocking.blockId)}`)
      setUnblocking(null)
      onChanged("ブロックを解除しました", "success")
      await mutate()
    } catch (unblockError) {
      onChanged(unblockError instanceof Error ? unblockError.message : "解除できませんでした", "error")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 860 }}>
      <div className="pad row" style={{ paddingBottom: 8 }}>
        <div className="grow"><h2 className="h2">ブロック中の IP</h2><div className="faint" style={{ fontSize: 12.5 }}>ログインとサインアップを、拒否します(アプリへのアクセスは、止めません)。</div></div>
        <button className="btn primary" type="button" onClick={onAdd}>IP をブロック</button>
      </div>
      <div className="tr th" style={cols}><span>範囲</span><span>理由</span><span>管理者</span><span>日時</span><span>期限</span><span>操作</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="取得できませんでした" onRetry={() => void mutate()} /></div>}
      {data && data.blocks.length === 0 && <div className="pad mut">ブロックしている IP はありません。</div>}
      {data?.blocks.map((block) => (
        <div key={block.blockId} className="tr" style={cols}>
          <span className="mono">{block.network}</span><span>{block.reason || <span className="faint">(理由なし)</span>}</span><span>{block.createdByName || block.createdBy}</span>
          <span className="mono faint">{shortDateTime(block.createdAt)}</span><span className="mono faint">{block.expiresAt ? shortDateTime(block.expiresAt) : "解除するまで"}</span>
          <span><button className="btn sm" type="button" onClick={() => setUnblocking(block)}>解除</button></span>
        </div>
      ))}
      {unblocking && (
        <Modal title="ブロックを解除しますか?" description="この範囲から、ログインできるようになります。" onClose={() => setUnblocking(null)}
          footer={<><button className="btn" type="button" onClick={() => setUnblocking(null)}>キャンセル</button><button className="btn primary" type="button" disabled={busy} onClick={() => void unblock()}>解除する</button></>}>
          <div className="mono">{unblocking.network}</div>
        </Modal>
      )}
    </div></div>
  )
}

/** 監査ログ(管理操作の記録)。 */
function AuditSection() {
  const [action, setAction] = useState("")
  const query = `limit=50${action ? `&action=${action}` : ""}`
  const { data, error, mutate } = useSWR<AuditList>(`/admin-api/security/audit-logs?${query}`, authenticatedFetcher, { refreshInterval: 15_000 })
  const [more, setMore] = useState<AuditLog[]>([])
  const [cursor, setCursor] = useState<string | null | undefined>(undefined)
  const nextCursor = cursor === undefined ? data?.nextCursor ?? null : cursor
  const cols = { "--cols": "160px 1fr 170px 1.2fr 1.4fr 130px" } as React.CSSProperties

  /** 次のページを読み足す。 */
  async function loadMore() {
    if (!nextCursor) return
    const page = await api.get<AuditList>(`/admin-api/security/audit-logs?${query}&cursor=${encodeURIComponent(nextCursor)}`)
    setMore((current) => [...current, ...page.logs])
    setCursor(page.nextCursor)
  }

  const logs = data ? [...data.logs, ...more] : []
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 980 }}>
      <div className="pad row" style={{ paddingBottom: 8, gap: 10, flexWrap: "wrap" }}>
        <div className="grow"><h2 className="h2">監査ログ</h2><div className="faint" style={{ fontSize: 12.5 }}>管理者の操作の記録です(追記だけで、変更も削除もできません)。</div></div>
        <select className="inp" style={{ width: "auto" }} value={action} onChange={(event) => { setAction(event.target.value); setMore([]); setCursor(undefined) }} aria-label="操作で絞り込む">
          <option value="">すべての操作</option>{Object.entries(ACTION_LABEL).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
        </select>
      </div>
      <div className="tr th" style={cols}><span>日時</span><span>管理者</span><span>操作</span><span>対象</span><span>補足</span><span>IP</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="取得できませんでした" onRetry={() => void mutate()} /></div>}
      {data && logs.length === 0 && <div className="pad mut">記録はありません。</div>}
      {logs.map((entry, index) => (
        <div key={`${entry.time}-${index}`} className="tr" style={cols}>
          <span className="mono faint">{shortDateTime(entry.time)}</span>
          <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }} title={entry.actorId}>{entry.actorName || entry.actorId}</span>
          <span>{ACTION_LABEL[entry.action] ?? entry.action}</span>
          <span className="mono" style={{ fontSize: 11.5, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }} title={entry.target}>{entry.target}</span>
          <span className="faint" style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={entry.detail}>{entry.detail}</span>
          <span className="mono">{entry.ip}</span>
        </div>
      ))}
      {nextCursor && <div className="pad"><button className="btn" type="button" onClick={() => void loadMore()}>もっと見る</button></div>}
    </div></div>
  )
}

/**
 * セキュリティ。不審な IP の確認、IP のブロックと解除、管理操作の監査ログ。
 */
export function AdminSecurity() {
  // ブロックの入力の画面(IP は、不審な IP の「ブロック」から入る)。
  const [adding, setAdding] = useState<{ network: string } | null>(null)
  const [reason, setReason] = useState("")
  const [expiresInHours, setExpiresInHours] = useState("24")
  const [busy, setBusy] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  /** 入力の IP をブロックする。 */
  async function block() {
    if (!adding) return
    setBusy(true)
    try {
      await api.post("/admin-api/security/blocked-ips", { network: adding.network.trim(), reason: reason.trim(), expiresInHours: Number(expiresInHours) })
      setAdding(null)
      setReason("")
      setRefreshKey((current) => current + 1)
      setToast({ message: "ブロックしました", type: "success" })
    } catch (blockError) {
      setToast({ message: blockError instanceof Error ? blockError.message : "ブロックできませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SuspiciousSection onBlock={(network) => setAdding({ network })} />
      <BlockedSection onAdd={() => setAdding({ network: "" })} refreshKey={refreshKey} onChanged={(message, type) => { setToast({ message, type }); setRefreshKey((current) => current + 1) }} />
      <AuditSection />
      {adding && (
        <Modal title="IP をブロックする" description="この IP(または範囲)から、ログインとサインアップができなくなります。あなた自身の IP は、ブロックできません。" onClose={() => setAdding(null)}
          footer={<><button className="btn" type="button" onClick={() => setAdding(null)}>キャンセル</button><button className="btn danger" type="button" disabled={busy || !adding.network.trim()} onClick={() => void block()}>ブロックする</button></>}>
          <label className="lbl">IP または範囲(CIDR)<input className="inp mono" value={adding.network} onChange={(event) => setAdding({ network: event.target.value })} placeholder="203.0.113.5 か 203.0.113.0/24" /></label>
          <label className="lbl">理由<input className="inp" value={reason} maxLength={100} onChange={(event) => setReason(event.target.value)} placeholder="例: 総当たりのログイン" /></label>
          <label className="lbl">期限<select className="inp" value={expiresInHours} onChange={(event) => setExpiresInHours(event.target.value)}>{EXPIRY_CHOICES.map((choice) => <option key={choice.value} value={choice.value}>{choice.label}</option>)}</select></label>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </>
  )
}

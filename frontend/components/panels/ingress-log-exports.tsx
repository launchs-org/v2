"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import type { GoogleDriveStatus, LogExport, LogExportTestResult } from "@/lib/types"
import { CopyButton } from "@/components/api-token-section"

/** webhook の送る間隔として選べる値(秒。サーバーと同じ)。 */
const INTERVAL_CHOICES = [{ value: 10, label: "10 秒ごと" }, { value: 30, label: "30 秒ごと" }, { value: 60, label: "1 分ごと" }, { value: 300, label: "5 分ごと" }] as const

/** 1 つのアカウントが持てる、転送の設定の数の上限(サーバーと同じ)。 */
const MAX_EXPORTS = 10

/**
 * 転送の状態を、バッジの言葉と色にする。
 *
 * @param item 転送の設定。
 * @returns 言葉と、バッジの色のクラス。
 */
function statusView(item: LogExport): { label: string; badge: string } {
  // 止めているものが、一番に分かるようにする。
  if (!item.enabled) return { label: "停止中", badge: "b-dim" }
  if (item.status === "reauth_required") return { label: "再接続が必要", badge: "b-warn" }
  if (item.status === "failing") return { label: "失敗中", badge: "b-bad" }
  return { label: "動作中", badge: "b-ok" }
}

/**
 * ingress の「アクセスログの転送」のカード(一覧、webhook の追加、Google Drive への転送、テスト送信、停止・再開、削除)。
 *
 * この ingress のアクセスログを、外(webhook か Google Drive)に書き出す。ログは 24 時間・10000 行で消えるので、残したいときに使う。
 * Google Drive は、アカウント設定でつないでいれば選べる(1 つの ingress に 1 つまで)。転送の設定は、アカウント全体で 10 個まで。
 *
 * @param props.basePath この ingress の API のパス(`/api/v1/projects/.../ingresses/...`)。
 * @param props.locked 削除中などで、操作できないとき。
 * @param props.notify 通知を出す関数。
 */
export function IngressLogExports({ basePath, locked, notify }: { basePath: string; locked: boolean; notify: (message: string, isError?: boolean) => void }) {
  const exportsPath = `${basePath}/log-exports`
  const { data, error, mutate } = useSWR<{ logExports: LogExport[] }>(exportsPath, authenticatedFetcher, { refreshInterval: 15000, revalidateOnFocus: true })
  const { data: google } = useSWR<GoogleDriveStatus>("/api/v1/account/google-drive", authenticatedFetcher, { revalidateOnFocus: true })
  const [url, setUrl] = useState("")
  const [intervalSeconds, setIntervalSeconds] = useState(30)
  const [busy, setBusy] = useState("")
  // 作った直後の webhook の署名の秘密(1 回だけ見られる。閉じると消える)。
  const [issuedSecret, setIssuedSecret] = useState<string | null>(null)
  const items = data?.logExports ?? []
  const hasDrive = items.some((item) => item.kind === "google_drive")

  /**
   * 操作を実行し、終わったら取り直す(失敗は通知する)。
   *
   * @param key 実行中の印(ボタンの無効化に使う)。
   * @param action 実行する処理。
   */
  async function run(key: string, action: () => Promise<void>) {
    // 実行中の印を付け、成功なら取り直し、失敗なら通知する。
    setBusy(key)
    try { await action(); await mutate() } catch (caught) { notify(caught instanceof Error ? caught.message : "操作に失敗しました", true) } finally { setBusy("") }
  }

  /** webhook を追加する(署名の秘密を、1 回だけ見せる)。 */
  async function addWebhook() {
    // 空なら何もしない。
    if (!url.trim()) return
    await run("add-webhook", async () => {
      const created = await api.post<LogExport>(exportsPath, { kind: "webhook", url: url.trim(), intervalSeconds })
      setUrl("")
      setIssuedSecret(created.secret ?? null)
      notify("webhook を追加しました。署名の秘密を、控えてください")
    })
  }

  /** Google Drive への転送を追加する。 */
  async function addDrive() {
    await run("add-drive", async () => { await api.post<LogExport>(exportsPath, { kind: "google_drive" }); notify("Google Drive への転送を追加しました") })
  }

  /**
   * テスト送信をする(webhook は test: true の本文を送る。Drive は、テスト用のファイルを作る)。
   *
   * @param item 転送の設定。
   */
  async function test(item: LogExport) {
    await run(item.logExportId, async () => {
      const result = await api.post<LogExportTestResult>(`${exportsPath}/${encodeURIComponent(item.logExportId)}/test`)
      notify(result.ok ? "テスト送信に成功しました" : `テスト送信に失敗しました: ${result.message}`, !result.ok)
    })
  }

  /**
   * 有効・無効を切り替える。
   *
   * @param item 転送の設定。
   */
  async function toggle(item: LogExport) {
    await run(item.logExportId, async () => { await api.patch(`${exportsPath}/${encodeURIComponent(item.logExportId)}`, { enabled: !item.enabled }) })
  }

  /**
   * 転送の設定を削除する(Drive に書き出したファイルは、残る)。
   *
   * @param item 転送の設定。
   */
  async function remove(item: LogExport) {
    await run(item.logExportId, async () => { await api.delete(`${exportsPath}/${encodeURIComponent(item.logExportId)}`); notify("転送を削除しました") })
  }

  return (
    <div className="card" style={{ overflow: "hidden" }}>
      <div className="row pad" style={{ paddingBottom: 4 }}><h2 className="h2 grow">アクセスログの転送</h2><span className="faint" style={{ fontSize: 12.5 }}>アカウント全体で {MAX_EXPORTS} 個まで</span></div>
      <div className="mut" style={{ fontSize: 12.5, padding: "0 18px 10px" }}>
        アクセスログは、24 時間(最大 10000 行)で消えます。残したいときは、外に書き出します。ログには、URL(クエリを含む)と、クライアントの IP が入ります。送り先は、信頼できる場所にしてください。
      </div>
      {error && !data && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>転送の設定を取得できませんでした。</div>}
      {data && items.length === 0 && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>転送の設定はありません。</div>}
      {items.map((item) => {
        const view = statusView(item)
        return (
          <div key={item.logExportId} style={{ padding: "12px 18px", borderTop: "1px solid var(--line)", display: "flex", flexDirection: "column", gap: 6 }}>
            <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
              <b>{item.kind === "google_drive" ? "Google Drive" : "webhook"}</b>
              {item.kind === "webhook" && <span className="mono grow" style={{ overflowWrap: "anywhere", fontSize: 12.5 }}>{item.url}</span>}
              {item.kind === "google_drive" && <span className="grow" />}
              <span className={`badge ${view.badge}`}><i />{view.label}</span>
              <button className="btn sm" type="button" disabled={locked || busy !== ""} onClick={() => void test(item)}>{busy === item.logExportId ? "処理中…" : "テスト送信"}</button>
              <button className="btn sm" type="button" disabled={locked || busy !== ""} onClick={() => void toggle(item)}>{item.enabled ? "停止する" : "再開する"}</button>
              <button className="btn sm danger" type="button" disabled={locked || busy !== ""} onClick={() => void remove(item)}>削除</button>
            </div>
            <div className="faint" style={{ fontSize: 12 }}>
              {item.kind === "webhook" && item.intervalSeconds ? `${INTERVAL_CHOICES.find((choice) => choice.value === item.intervalSeconds)?.label ?? `${item.intervalSeconds} 秒ごと`} · ` : ""}
              {item.kind === "google_drive" ? "launchs-access-logs / プロジェクト名 / ホスト名_日付.ndjson に、1 時間ごと · " : ""}
              {item.lastSuccessAt ? `最後に送れた日時: ${shortDateTime(item.lastSuccessAt)}` : "まだ送れていません"}
              {item.failureCount > 0 ? ` · 失敗が ${item.failureCount} 回続いています` : ""}
            </div>
            {item.lastError && item.enabled && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{item.lastError}</div>}
          </div>
        )
      })}
      {issuedSecret && (
        <div className="col" style={{ padding: "12px 18px", borderTop: "1px solid var(--line)", gap: 6, background: "var(--warn-soft, transparent)" }}>
          <b>署名の秘密(今だけ見られます)</b>
          <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
            <code className="mono grow" style={{ overflowWrap: "anywhere" }}>{issuedSecret}</code>
            <CopyButton text={issuedSecret} />
            <button className="btn sm" type="button" onClick={() => setIssuedSecret(null)}>閉じる</button>
          </div>
          <div className="faint" style={{ fontSize: 12 }}>受け取る側で、ヘッダー <span className="mono">X-Launchs-Signature</span>(<span className="mono">sha256=</span> + HMAC-SHA256(秘密, タイムスタンプ + &quot;.&quot; + 本文))を確かめてください。閉じると、もう見られません。</div>
        </div>
      )}
      <div className="col" style={{ padding: "12px 18px 14px", borderTop: "1px solid var(--line)", gap: 10 }}>
        <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
          <input className="inp mono grow" style={{ minWidth: 220 }} value={url} onChange={(event) => setUrl(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void addWebhook() }}
            placeholder="https://example.com/hooks/launchs" aria-label="webhook の URL" disabled={locked || items.length >= MAX_EXPORTS} />
          <select className="inp" value={intervalSeconds} onChange={(event) => setIntervalSeconds(Number(event.target.value))} aria-label="送る間隔" disabled={locked}>
            {INTERVAL_CHOICES.map((choice) => <option key={choice.value} value={choice.value}>{choice.label}</option>)}
          </select>
          <button className="btn sm primary" type="button" disabled={locked || busy !== "" || !url.trim() || items.length >= MAX_EXPORTS} onClick={() => void addWebhook()}>webhook を追加</button>
        </div>
        <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
          <button className="btn sm" type="button" disabled={locked || busy !== "" || hasDrive || !google?.connected || items.length >= MAX_EXPORTS} onClick={() => void addDrive()}>Google Drive に転送する</button>
          <span className="faint" style={{ fontSize: 12 }}>
            {hasDrive ? "この ingress には、すでに Google Drive への転送があります。"
              : google && !google.available && !google.connected ? "このサーバーでは、Google Drive は設定されていません。"
              : google && !google.connected ? "先に、アカウント設定で、Google Drive に接続してください。"
              : "Google Drive は、1 時間ごとに、日付ごとのファイルへ書き出します。"}
          </span>
        </div>
      </div>
    </div>
  )
}

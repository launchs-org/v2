"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { GoogleDriveStatus } from "@/lib/types"
import { ToastNotice } from "@/components/ui-kit"

/** Google の同意のあとに、画面へ戻ってきたときの結果(URL の ?google=…)を、知らせる言葉にする。 */
const RETURN_MESSAGE: Record<string, { message: string; type: "success" | "error" }> = {
  connected: { message: "Google Drive に接続しました", type: "success" },
  denied: { message: "Google Drive への接続は、許可されませんでした", type: "error" },
  error: { message: "Google Drive に接続できませんでした。もう一度、お試しください", type: "error" },
}

/**
 * アカウント設定の「Google Drive」(接続、再接続、解除)。
 *
 * ここでつないでおくと、各 ingress の「アクセスログの転送」で、Google Drive を選べる。
 * 権限は「launchs が作ったファイルだけ」(drive.file)。Drive の、ほかのファイルは、見えない。
 *
 * 処理の流れ:
 *  1. 「接続する」を押すと、サーバーが Google の同意の画面の URL を返し、その画面に移る。
 *  2. 同意すると、サーバーに戻り、そこから、この画面(`/?google=connected`)に戻ってくる。
 *  3. 合鍵が効かなくなったとき(許可を取り消した、など)は、「再接続が必要」と出る。もう一度、接続する。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function GoogleDriveSection({ refreshInterval }: { refreshInterval: number }) {
  const { data, error, mutate } = useSWR<GoogleDriveStatus>("/api/v1/account/google-drive", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  // 同意の画面から「戻る」で戻ってきたとき(ブラウザが、ページを使い回すとき)に、処理中のまま固まらないようにする。
  useEffect(() => {
    const release = () => setBusy(false)
    window.addEventListener("pageshow", release)
    return () => window.removeEventListener("pageshow", release)
  }, [])

  // Google から戻ってきた結果を、知らせる(知らせたら、URL から消す)。
  useEffect(() => {
    const result = new URLSearchParams(window.location.search).get("google")
    if (!result) return
    const view = RETURN_MESSAGE[result]
    if (view) setToast(view)
    window.history.replaceState(null, "", window.location.pathname)
    void mutate()
  }, [mutate])

  /** Google の同意の画面に移る(再接続も同じ)。 */
  async function connect() {
    // 二重に押されないよう、処理中にする。
    setBusy(true)
    try {
      const result = await api.post<{ url: string }>("/api/v1/account/google-drive/authorize")
      window.location.href = result.url
      // 同じ画面の中の移動(見せかけのモード)では、画面が開き直らないので、処理中を戻す。
      if (result.url.startsWith("#")) setBusy(false)
    } catch (connectError) {
      setToast({ message: connectError instanceof Error ? connectError.message : "接続を始められませんでした", type: "error" })
      setBusy(false)
    }
  }

  /** 接続を解除する(この接続を使う転送も、消える。Drive に書き出したファイルは、残る)。 */
  async function disconnect() {
    setBusy(true)
    try {
      await api.delete("/api/v1/account/google-drive")
      setConfirming(false)
      setToast({ message: "Google Drive の接続を解除しました", type: "success" })
      await mutate()
    } catch (disconnectError) {
      setToast({ message: disconnectError instanceof Error ? disconnectError.message : "解除できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  const needsReconnect = data?.connected && data.status === "reauth_required"
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div>
        <h2 className="h2">Google Drive</h2>
        <div className="faint" style={{ fontSize: 12.5 }}>
          ここでつないでおくと、ingress の「アクセスログの転送」で、Google Drive を選べます。launchs が作ったファイル(<span className="mono">launchs-access-logs</span> フォルダの中)だけに触れます。Drive のほかのファイルは、見えません。
        </div>
      </div>
      {error && !data && <div className="mut">状態を取得できませんでした。</div>}
      {data && !data.connected && (
        <div className="row" style={{ gap: 10, flexWrap: "wrap" }}>
          <span className="badge b-dim"><i />未接続</span>
          <span className="grow" />
          {data.available
            ? <button className="btn primary" type="button" disabled={busy} onClick={() => void connect()}>{busy ? "移動中…" : "Google Drive に接続する"}</button>
            : <span className="faint" style={{ fontSize: 12.5 }}>このサーバーでは、Google Drive は設定されていません(管理者に、お問い合わせください)。</span>}
        </div>
      )}
      {data?.connected && (
        <div className="col" style={{ gap: 10 }}>
          <div className="row" style={{ gap: 10, flexWrap: "wrap" }}>
            <span className={`badge ${needsReconnect ? "b-warn" : "b-ok"}`}><i />{needsReconnect ? "再接続が必要" : "接続中"}</span>
            <span className="mono grow" style={{ overflowWrap: "anywhere" }}>{data.accountEmail || "(メールアドレス不明)"}</span>
            {needsReconnect && <button className="btn primary" type="button" disabled={busy || !data.available} onClick={() => void connect()}>再接続する</button>}
            {!confirming && <button className="btn danger" type="button" disabled={busy} onClick={() => setConfirming(true)}>解除する</button>}
          </div>
          {needsReconnect && data.lastError && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{data.lastError}</div>}
          {confirming && (
            <div className="row" style={{ gap: 8, flexWrap: "wrap", background: "var(--warn-soft, transparent)", padding: "8px 10px", borderRadius: 8 }}>
              <span className="grow" style={{ fontSize: 12.5 }}>解除すると、この接続を使う転送(Google Drive 宛)は、すべて消えます。Drive に書き出したファイルは、残ります。</span>
              <button className="btn" type="button" disabled={busy} onClick={() => setConfirming(false)}>やめる</button>
              <button className="btn danger" type="button" disabled={busy} onClick={() => void disconnect()}>解除する</button>
            </div>
          )}
        </div>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div>
  )
}

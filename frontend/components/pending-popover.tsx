"use client"

import { useState } from "react"
import { api } from "@/lib/api"
import type { Nav } from "@/lib/nav"
import type { PendingItem, PendingSummary } from "@/lib/pending"
import { ErrorState } from "@/components/ui-kit"
import { KindIcon, type Kind } from "@/components/kind-icon"

/** 未適用の変更の種類から、リソースの種類(色つきアイコン用)と、開くタブを決める。 */
const KIND_OF: Record<PendingItem["kind"], { kind: Kind; tab?: "config" | "mounts" | "service" }> = {
  "起動設定": { kind: "deploy", tab: "config" },
  "マウント": { kind: "deploy", tab: "mounts" },
  Service: { kind: "service", tab: "service" },
  Ingress: { kind: "ingress" },
  "ボリューム": { kind: "volume" },
}

/**
 * 「反映待ち N 件」のボタンと、そのポップオーバー(個別・一括の、反映と破棄)。
 * 以前の「概要」の Apply センターの代わり。
 *
 * @param props.pending 未適用の変更の集計(usePending の結果)。
 * @param props.nav 画面を移る操作(対象を押すと、そのパネルを開く)。
 */
export function PendingPopover({ pending, nav }: { pending: PendingSummary; nav: Nav }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const items = pending.items
  if (!items.length) return null

  /**
   * 1件、または全件に、反映か破棄を頼む(順に呼ぶ)。
   *
   * @param targets 対象。
   * @param action "apply" か "discard"。
   */
  async function run(targets: PendingItem[], action: "apply" | "discard") {
    setBusy(true)
    setError("")
    try {
      for (const item of targets) await api.post(action === "apply" ? item.applyPath : item.discardPath)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "操作を完了できませんでした")
    } finally {
      await pending.refresh()
      setBusy(false)
    }
  }

  /** 対象を押したとき: そのリソースのパネルを開く。 */
  function openItem(item: PendingItem) {
    setOpen(false)
    if (item.deployId) nav.openDeploy(item.deployId, KIND_OF[item.kind].tab)
  }

  return (
    <div style={{ position: "relative" }}>
      <button className="btn sm" type="button" onClick={() => setOpen((current) => !current)} aria-expanded={open}><span className="pdot" />反映待ち {items.length} 件</button>
      {open && (
        <div className="popover" role="dialog" aria-label="反映待ちの変更">
          <div className="row" style={{ padding: "8px 10px" }}>
            <b className="grow">反映待ちの変更</b>
            <button className="btn warn sm" type="button" onClick={() => void run(items, "apply")} disabled={busy}>すべて反映</button>
          </div>
          {error && <div style={{ padding: "0 10px 8px" }}><ErrorState message={error} /></div>}
          {items.map((item) => (
            <div key={item.key} className="row" style={{ padding: "8px 10px", borderTop: "1px solid var(--line)", flexWrap: "nowrap", gap: 10 }}>
              <KindIcon kind={KIND_OF[item.kind].kind} size={28} />
              <div style={{ minWidth: 0, flex: 1 }}>
                <button className="lnk mono" type="button" style={{ maxWidth: "100%", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} onClick={() => openItem(item)}>{item.target}</button>
                <div className="mut" style={{ fontSize: 12 }}>{item.kind}: {item.sub}</div>
              </div>
              <button className="btn sm" type="button" onClick={() => void run([item], "discard")} disabled={busy}>破棄</button>
              <button className="btn sm warn" type="button" onClick={() => void run([item], "apply")} disabled={busy}>反映</button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

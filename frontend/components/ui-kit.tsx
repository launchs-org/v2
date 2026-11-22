"use client"

import { useEffect, useRef } from "react"
import { AlertCircle, Check, X } from "lucide-react"

/** 状態を、バッジの色と、表示する言葉にする(モックの b-ok / b-info / b-warn / b-bad / b-dim)。live は、変化の途中で、点が点滅する。 */
const STATUS_VIEW: Record<string, { label: string; cls: string; live?: boolean }> = {
  active: { label: "稼働中", cls: "b-ok" },
  success: { label: "成功", cls: "b-ok" },
  running: { label: "実行中", cls: "b-ok" },
  pending: { label: "準備中", cls: "b-info", live: true },
  creating: { label: "作成中", cls: "b-info", live: true },
  applying: { label: "反映中", cls: "b-warn", live: true },
  scaling: { label: "スケール中", cls: "b-warn", live: true },
  deleting: { label: "削除中", cls: "b-warn", live: true },
  canceling: { label: "キャンセル中", cls: "b-warn", live: true },
  failed: { label: "失敗", cls: "b-bad" },
  delete_failed: { label: "削除失敗", cls: "b-bad" },
  scaled_zero: { label: "停止(0台)", cls: "b-dim" },
  canceled: { label: "キャンセル済み", cls: "b-dim" },
  banned: { label: "停止中", cls: "b-bad" },
  user: { label: "ユーザー", cls: "b-dim" },
  admin: { label: "管理者", cls: "b-info" },
}

/**
 * 状態のバッジ。
 *
 * @param props.status API の状態の文字列(知らない値は、そのまま、灰色で出す)。
 */
export function Badge({ status }: { status: string }) {
  const view = STATUS_VIEW[status] ?? { label: status, cls: "b-dim" }
  return <span className={`badge ${view.cls} ${view.live ? "live" : ""}`}><i />{view.label}</span>
}

/** 以前の名前(互換)。 */
export const StatusPill = Badge

/**
 * 状態が、変化の途中か(操作を、受け付けない状態か)。
 *
 * @param status deploy などの状態。
 * @returns 作成中・反映中・スケール中・削除中なら true。
 */
export function isBusyStatus(status: string): boolean {
  return ["pending", "creating", "applying", "scaling", "deleting"].includes(status)
}

/** 画面の見出し(題と、右側の操作)。 */
export function PageHeading({ title, actions }: { eyebrow?: string; title: string; description?: string; actions?: React.ReactNode }) {
  return <div className="row"><h1 className="h1 disp grow">{title}</h1>{actions}</div>
}

/** 「未適用の変更があります」の帯(破棄と反映のボタンつき)。 */
export function ApplyBar({ title, hint, busy, onDiscard, onApply }: { title: string; hint?: string; busy: boolean; onDiscard: () => void; onApply: () => void }) {
  return (
    <div className="applybar">
      <span className="pdot" />
      <div className="grow"><b>{title}</b>{hint && <div className="mut" style={{ fontSize: 13 }}>{hint}</div>}</div>
      <button className="btn" type="button" onClick={onDiscard} disabled={busy}>破棄</button>
      <button className="btn warn" type="button" onClick={onApply} disabled={busy}>反映</button>
    </div>
  )
}

/** 差分の表示(+ 追加、~ 変更、- 削除)。 */
export function DiffBox({ lines }: { lines: { cls: "d-add" | "d-chg" | "d-del"; text: string }[] }) {
  return <div className="diff">{lines.map((line, index) => <div key={index} className={line.cls}>{line.text}</div>)}</div>
}

export function EmptyState({ title, description, action }: { icon?: React.ReactNode; title: string; description?: string; action?: React.ReactNode }) {
  return <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 10, alignItems: "flex-start" }}><h2 className="h2">{title}</h2>{description && <div className="mut">{description}</div>}{action}</div>
}

export function LoadingState({ label = "読み込んでいます" }: { label?: string }) {
  return <div className="row mut" style={{ padding: 16 }}><span className="loader" />{label}</div>
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px", gap: 10 }}><AlertCircle size={15} />{message}{onRetry && <button className="btn sm" type="button" onClick={onRetry}>再試行</button>}</div>
}

export function Modal({ title, description, onClose, children, footer, wide = false }: { title: string; description?: string; onClose: () => void; children: React.ReactNode; footer?: React.ReactNode; wide?: boolean }) {
  const dialogRef = useRef<HTMLElement>(null)
  const closeRef = useRef(onClose)
  closeRef.current = onClose

  useEffect(() => {
    const previousActive = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = "hidden"
    const dialog = dialogRef.current
    const focusable = () => dialog?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])')
    window.requestAnimationFrame(() => (focusable()?.[0] ?? dialog)?.focus())

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        closeRef.current()
        return
      }
      if (event.key !== "Tab" || !dialog) return
      const items = Array.from(focusable() ?? [])
      if (!items.length) {
        event.preventDefault()
        dialog.focus()
      } else if (event.shiftKey && document.activeElement === items[0]) {
        event.preventDefault()
        items[items.length - 1].focus()
      } else if (!event.shiftKey && document.activeElement === items[items.length - 1]) {
        event.preventDefault()
        items[0].focus()
      }
    }

    window.addEventListener("keydown", handleKeyDown)
    return () => {
      window.removeEventListener("keydown", handleKeyDown)
      document.body.style.overflow = previousOverflow
      previousActive?.focus()
    }
  }, [])

  return (
    <div className="modalbg" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section ref={dialogRef} className={`modal ${wide ? "wide" : ""}`} role="dialog" aria-modal="true" aria-labelledby="modal-title" aria-describedby={description ? "modal-description" : undefined} tabIndex={-1}>
        <div className="row"><h2 id="modal-title" className="h2 grow" style={{ fontSize: 18 }}>{title}</h2><button className="btn ico ghost" type="button" onClick={onClose} aria-label="閉じる"><X size={18} /></button></div>
        {description && <div id="modal-description" className="mut">{description}</div>}
        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>{children}</div>
        {footer && <div className="row" style={{ justifyContent: "flex-end" }}>{footer}</div>}
      </section>
    </div>
  )
}

export function ToastNotice({ message, type = "success", onDismiss }: { message: string; type?: "success" | "error"; onDismiss: () => void }) {
  useEffect(() => {
    const timer = window.setTimeout(onDismiss, 4200)
    return () => window.clearTimeout(timer)
  }, [message, onDismiss])
  if (!message) return null
  return <div className="toast" role={type === "error" ? "alert" : "status"} style={type === "error" ? { background: "#B3261E" } : undefined}>{type === "error" ? <AlertCircle size={16} /> : <Check size={16} />}<span>{message}</span><button className="btn sm ghost" type="button" onClick={onDismiss} style={{ color: "#fff" }}>閉じる</button></div>
}

export function formattedDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? "—" : new Intl.DateTimeFormat("ja-JP", { dateStyle: "medium", timeStyle: "short" }).format(date)
}

export function initials(value: string) {
  return value.slice(0, 1).toUpperCase() || "L"
}

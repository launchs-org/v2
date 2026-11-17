"use client"

import { useEffect } from "react"
import { AlertCircle, Check, LoaderCircle, X } from "lucide-react"

export function PageHeading({ eyebrow, title, description, actions }: { eyebrow: string; title: string; description?: string; actions?: React.ReactNode }) {
  return (
    <div className="page-head">
      <div>
        <div className="page-kicker">{eyebrow}</div>
        <h1 className="page-title">{title}</h1>
        {description && <p className="page-description">{description}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </div>
  )
}

const statusText: Record<string, string> = {
  active: "稼働中", success: "成功", pending: "準備中", creating: "作成中", applying: "反映中", scaling: "スケール中",
  running: "実行中", failed: "失敗", delete_failed: "削除失敗", deleting: "削除中", canceling: "キャンセル中", canceled: "キャンセル済み",
  banned: "停止中", user: "ユーザー", admin: "管理者",
}

export function StatusPill({ status }: { status: string }) {
  const klass = ["active", "success"].includes(status) ? "active" : ["pending", "creating", "applying", "scaling", "running"].includes(status) ? "progress" : status
  return <span className={`status-pill ${klass}`}>{statusText[status] || status}</span>
}

export function EmptyState({ icon, title, description, action }: { icon: React.ReactNode; title: string; description: string; action?: React.ReactNode }) {
  return <div className="empty-state"><div className="empty-icon">{icon}</div><strong>{title}</strong><p>{description}</p>{action}</div>
}

export function LoadingState({ label = "データを読み込んでいます" }: { label?: string }) {
  return <div className="loading-line"><span className="loader" />{label}</div>
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return <div className="api-error" role="alert"><AlertCircle size={14} style={{ verticalAlign: "-3px", marginRight: 6 }} />{message}{onRetry && <button className="text-link" onClick={onRetry} type="button">再試行</button>}</div>
}

export function Modal({ title, description, onClose, children, footer }: { title: string; description?: string; onClose: () => void; children: React.ReactNode; footer?: React.ReactNode }) {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose()
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [onClose])

  return (
    <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section className="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title">
        <div className="modal-head"><div><h2 id="modal-title">{title}</h2>{description && <p>{description}</p>}</div><button className="icon-button" type="button" onClick={onClose} aria-label="閉じる"><X size={16} /></button></div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
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
  return <div className={`toast ${type === "error" ? "error" : ""}`} role={type === "error" ? "alert" : "status"}>{type === "error" ? <AlertCircle size={14} /> : <Check size={14} />}{message}<button className="icon-button" onClick={onDismiss} type="button" aria-label="通知を閉じる"><X size={14} /></button></div>
}

export function formattedDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? "—" : new Intl.DateTimeFormat("ja-JP", { dateStyle: "medium", timeStyle: "short" }).format(date)
}

export function initials(value: string) {
  return value.slice(0, 1).toUpperCase() || "L"
}

"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { revalidateProject } from "@/lib/revalidate"
import type { ProjectViewProps } from "@/lib/nav"
import { usePending } from "@/lib/pending"
import type { IngressDetail, IngressRule } from "@/lib/types"
import { ApplyBar, Badge, DiffBox, ErrorState, LoadingState, Modal, ToastNotice, isBusyStatus } from "@/components/ui-kit"
import { TagEditor } from "@/components/tag-editor"
import { PanelFrame } from "@/components/panels/panel-frame"
import { IngressAccessLog, updateAccessLogEnabled } from "@/components/panels/ingress-access-log"
import { IngressDomains } from "@/components/panels/ingress-domains"

/** ルールの転送先に選べる、Service のポート 1 つ。 */
type ServiceChoice = { serviceId: string; label: string; ports: number[] }

/**
 * ルール 1 件を、差分表示用の 1 行の文字にする。
 *
 * @param rule ルール。
 * @returns 「path → serviceId:port」の形の文字列。
 */
function ruleText(rule: IngressRule): string {
  // path、Service、ポートを 1 行にする。
  return `${rule.path} → ${rule.serviceId}:${rule.port}`
}

/**
 * 現在のルールと、未適用のルールの差分を作る(追加・変更・削除)。
 *
 * @param current 現在のルール。
 * @param pending 未適用のルール。
 * @returns 差分の行。
 */
function diffRules(current: IngressRule[], pending: IngressRule[]) {
  const lines: { cls: "d-add" | "d-chg" | "d-del"; text: string }[] = []
  // path ごとに、現在と未適用を引き比べる。
  const currentByPath = new Map(current.map((rule) => [rule.path, rule]))
  const pendingByPath = new Map(pending.map((rule) => [rule.path, rule]))
  for (const rule of pending) {
    const before = currentByPath.get(rule.path)
    if (!before) lines.push({ cls: "d-add", text: `+ ${ruleText(rule)}` })
    else if (before.serviceId !== rule.serviceId || before.port !== rule.port) lines.push({ cls: "d-chg", text: `~ ${ruleText(before)}   →   ${ruleText(rule)}` })
  }
  for (const rule of current) {
    if (!pendingByPath.has(rule.path)) lines.push({ cls: "d-del", text: `- ${ruleText(rule)}` })
  }
  return lines
}

/**
 * ingress の右パネル(ホスト、状態、ルールの編集、反映待ちの帯、削除)。
 *
 * 処理の流れ:
 *  1. ingress の詳細と、転送先に選べる Service(稼働中で TCP のポートがあるもの)を取る。
 *  2. ルールを下書きで編集し、保存(未適用)→ 反映の順で、公開に反映する。
 *  3. ingress が無くなった(削除が終わった)ら、パネルを閉じる。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.ingressId ingress の ID。
 * @param props.onClose パネルを閉じるとき。
 */
export function IngressPanel({ project, refreshInterval, ingressId, onClose }: ProjectViewProps & { ingressId: string; onClose: () => void }) {
  const path = `/api/v1/projects/${encodeURIComponent(project.projectId)}/ingresses/${encodeURIComponent(ingressId)}`
  const { data: detail, error, mutate } = useSWR<IngressDetail>(path, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { deployDetails } = usePending(project, refreshInterval)
  const [draft, setDraft] = useState<IngressRule[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [problem, setProblem] = useState("")
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)

  // 転送先に選べる Service(稼働中で、TCP のポートがあるものだけ。ingress は HTTP のみ)。
  const choices: ServiceChoice[] = []
  for (const deploy of deployDetails) {
    if (deploy.service?.status !== "active") continue
    const ports = deploy.service.ports.filter((port) => port.protocol === "tcp").map((port) => port.port)
    if (ports.length) choices.push({ serviceId: deploy.service.serviceId, label: `${deploy.imageRef.split("/").pop()} · ${deploy.service.serviceId.slice(0, 16)}`, ports })
  }

  /**
   * 通知を出す。
   *
   * @param message 本文。
   * @param isError 失敗なら true。
   */
  function notify(message: string, isError = false) { setToast({ message, error: isError }) }

  /**
   * 操作を実行し、終わったら取り直す(失敗は通知する)。
   *
   * @param action 実行する処理。
   */
  async function run(action: () => Promise<void>) {
    // 実行中の印を付け、成功なら取り直し、失敗なら通知する。
    setBusy(true)
    try { await action(); await mutate() } catch (caught) { notify(caught instanceof Error ? caught.message : "操作に失敗しました", true) } finally { setBusy(false) }
  }

  // 削除が終わって(404 になって)、ingress が無くなったら(取れなくなったら)、パネルを閉じる。
  // 取れなくなったら(削除が終わった、など)、パネルを閉じる(描画の途中ではなく、描画の後に閉じる)。
  useEffect(() => { if (error && (!detail || (error as { status?: number }).status === 404)) onClose() }, [error, detail, onClose])
  if (!detail) return <PanelFrame kind="ingress" title={ingressId} onClose={onClose}><LoadingState /></PanelFrame>
  // 反映中と削除中は、編集も削除もできない(サーバーも 409)。
  const locked = isBusyStatus(detail.status)
  const saved = detail.pending_rules ?? detail.rules
  const rows = draft ?? saved
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(saved)
  const firstChoice = choices[0]
  const pendingLines = detail.pending_rules ? diffRules(detail.rules, detail.pending_rules) : []

  /**
   * 下書きの行を 1 つ書き換える。
   *
   * @param index 行の番号。
   * @param updates 変える項目。
   */
  function updateRow(index: number, updates: Partial<IngressRule>) { setDraft(rows.map((rule, at) => (at === index ? { ...rule, ...updates } : rule))) }

  /** 下書きを保存する(全件を送って、未適用の変更にする)。 */
  async function save() {
    // path は「/」で始まり、転送先が決まっていること。
    if (rows.some((rule) => !rule.path.startsWith("/") || !rule.serviceId || !(rule.port >= 1 && rule.port <= 65535))) { setProblem("path は「/」から始め、Service とポートを指定してください。"); return }
    setProblem("")
    await run(async () => { await api.put(path, { rules: rows }); setDraft(null); notify("ルールを保存しました(未適用)") })
  }

  return (
    <>
      <PanelFrame
        kind="ingress"
        title={detail.host}
        subtitle={detail.ingressId}
        badge={<Badge status={detail.status} />}
        actions={<button className="btn sm danger" type="button" disabled={locked || busy} onClick={() => setConfirming(true)}>削除</button>}
        onClose={onClose}
      >
        {/* 公開 URL。 */}
        <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <span className="faint" style={{ fontSize: 12 }}>公開 URL</span>
          <a className="mono" style={{ fontWeight: 500, color: "var(--accent)", overflowWrap: "anywhere" }} href={`https://${detail.host}`} target="_blank" rel="noreferrer">https://{detail.host}</a>
        </div>
        {/* カスタムドメイン(自分のドメインを、DNS で持ち主を確かめてから、使えるようにする)。 */}
        <IngressDomains basePath={path} locked={detail.status === "deleting"} notify={notify} />
        {/* アクセスログ(1 リクエストごとの表。日時、パス、IP、ステータスなどで絞り込める)。 */}
        <div className="card" style={{ overflow: "hidden" }}>
          <IngressAccessLog basePath={path} enabled={detail.accessStats !== false} locked={locked || busy}
            onToggle={(next) => void run(async () => { await updateAccessLogEnabled(path, next); notify(next ? "アクセスログの記録を有効にしました" : "アクセスログの記録を止めました") })} />
        </div>
        {/* ルールの編集。 */}
        <div className="card" style={{ overflow: "hidden" }}>
          <div className="row pad" style={{ paddingBottom: 8 }}>
            <h2 className="h2 grow">ルール</h2>
            <button className="btn sm" type="button" disabled={locked || busy} onClick={() => setDraft([...rows, { path: "/", serviceId: firstChoice?.serviceId ?? "", port: firstChoice?.ports[0] ?? 80 }])}>+ ルール追加</button>
          </div>
          <div className="mut" style={{ fontSize: 12.5, padding: "0 18px 8px" }}>path ごとに、届け先の Service とポートを決めます(HTTP のみ)。</div>
          {rows.length === 0 && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>ルールがありません。</div>}
          {rows.map((rule, index) => {
            const isNew = !detail.rules.some((current) => current.path === rule.path)
            const isChanged = !isNew && detail.rules.some((current) => current.path === rule.path && (current.serviceId !== rule.serviceId || current.port !== rule.port))
            const state = isNew ? "new" : isChanged ? "chg" : ""
            const known = choices.some((choice) => choice.serviceId === rule.serviceId)
            return (
              <div className="tr" key={index} style={{ ["--cols" as string]: "1fr 1.6fr 84px 40px" }}>
                <input className={`inp mono ${state}`} value={rule.path} onChange={(event) => updateRow(index, { path: event.target.value })} disabled={locked} aria-label="パス" placeholder="/" />
                <select className={`inp mono ${state}`} value={rule.serviceId} onChange={(event) => updateRow(index, { serviceId: event.target.value, port: choices.find((choice) => choice.serviceId === event.target.value)?.ports[0] ?? rule.port })} disabled={locked} aria-label="Service">
                  {!known && <option value={rule.serviceId}>{rule.serviceId || "(選んでください)"}</option>}
                  {choices.map((choice) => <option key={choice.serviceId} value={choice.serviceId}>{choice.label}</option>)}
                </select>
                <input className={`inp mono ${state}`} type="number" value={Number.isNaN(rule.port) ? "" : rule.port} onChange={(event) => updateRow(index, { port: event.target.value === "" ? Number.NaN : Number(event.target.value) })} disabled={locked} aria-label="ポート" />
                <button className="btn ico ghost" type="button" disabled={locked} onClick={() => setDraft(rows.filter((_, at) => at !== index))} aria-label="ルールを削除">×</button>
              </div>
            )
          })}
          {problem && <div style={{ color: "var(--bad)", fontSize: 12.5, padding: "8px 18px 0" }}>{problem}</div>}
          {dirty && <div className="row" style={{ padding: "12px 18px 14px" }}><button className="btn sm primary" type="button" disabled={busy || locked} onClick={() => void save()}>保存</button><button className="btn sm" type="button" disabled={busy} onClick={() => { setDraft(null); setProblem("") }}>編集を戻す</button></div>}
        </div>
        {/* 反映待ち。 */}
        {detail.pending_rules && !dirty && (
          <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}><h2 className="h2">変更内容の確認</h2><DiffBox lines={pendingLines} /></div>
            <ApplyBar title="反映待ちの変更" busy={busy || locked} onDiscard={() => void run(async () => { await api.post(`${path}/discard`); notify("破棄しました") })} onApply={() => void run(async () => { await api.post(`${path}/apply`); notify("反映を開始しました") })} />
          </div>
        )}
        {/* タグは、一番下。 */}
        <TagEditor tags={detail.tags ?? []} save={async (next) => { await api.put(`${path}/tags`, { tags: next }); await revalidateProject(project.projectId) }} />
      </PanelFrame>
      {confirming && (
        <Modal title="Ingress を削除しますか?" description={detail.host} onClose={() => setConfirming(false)} footer={<><button className="btn" type="button" onClick={() => setConfirming(false)}>キャンセル</button><button className="btn warn" type="button" disabled={busy} onClick={() => void run(async () => { await api.delete(path); setConfirming(false); notify("削除を始めました"); await revalidateProject(project.projectId) })}>削除</button></>}>
          <div className="mut">ホスト名とルールが削除されます。</div>
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => setToast(null)} />}
    </>
  )
}

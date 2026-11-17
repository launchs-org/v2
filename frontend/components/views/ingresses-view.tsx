"use client"

import { useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ProjectViewProps } from "@/lib/nav"
import { usePending } from "@/lib/pending"
import type { IngressDetail, IngressListResponse, IngressRule } from "@/lib/types"
import { ApplyBar, Badge, DiffBox, ErrorState, LoadingState, Modal, ToastNotice, isBusyStatus } from "@/components/ui-kit"

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
 * Ingress の画面(モックの vIngresses)。ingress ごとのカードに、ルールの編集と、反映待ちの帯を出す。
 *
 * @param props.project プロジェクト。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.createTick 上のバーの「作成」が押された回数。
 */
export function IngressesView({ project, refreshInterval, createTick }: ProjectViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: list, error, mutate: refreshList } = useSWR<IngressListResponse>(`${base}/ingresses?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { deployDetails } = usePending(project, refreshInterval)
  const [toast, setToast] = useState("")
  const [toastError, setToastError] = useState(false)
  const lastTick = useRef(createTick)

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
  function notify(message: string, isError = false) {
    setToast(message)
    setToastError(isError)
  }

  // 上のバーの「作成」が押されたら、ingress を作る(ホスト名は自動で決まる)。
  useEffect(() => {
    if (createTick === lastTick.current) return
    lastTick.current = createTick
    void (async () => {
      try {
        await api.post(`${base}/ingresses`)
        await refreshList()
        notify("Ingress を作成しました")
      } catch (caught) {
        notify(caught instanceof Error ? caught.message : "Ingress を作成できませんでした", true)
      }
    })()
  }, [createTick, base, refreshList])

  return (
    <>
      <div><h1 className="h1 disp">Ingress</h1></div>
      {error && <ErrorState message="Ingress 一覧を取得できませんでした。" onRetry={() => void refreshList()} />}
      {!list && !error && <LoadingState />}
      {list && list.ingresses.length === 0 && <div className="mut">Ingress がありません。</div>}
      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        {list?.ingresses.map((item) => <IngressCard key={item.ingressId} base={base} ingressId={item.ingressId} refreshInterval={refreshInterval} choices={choices} onChanged={() => void refreshList()} notify={notify} />)}
      </div>
      {toast && <ToastNotice message={toast} type={toastError ? "error" : "success"} onDismiss={() => setToast("")} />}
    </>
  )
}

/**
 * ingress 1 件のカード(ホスト、状態、ルールの編集、反映待ちの帯)。
 *
 * @param props.base プロジェクトの API のパス。
 * @param props.ingressId ingress の ID。
 * @param props.refreshInterval 自動更新の間隔。
 * @param props.choices 転送先の候補。
 * @param props.onChanged 削除などで一覧が変わったとき。
 * @param props.notify 通知を出す関数。
 */
function IngressCard({ base, ingressId, refreshInterval, choices, onChanged, notify }: { base: string; ingressId: string; refreshInterval: number; choices: ServiceChoice[]; onChanged: () => void; notify: (message: string, isError?: boolean) => void }) {
  const path = `${base}/ingresses/${encodeURIComponent(ingressId)}`
  const { data: detail, mutate } = useSWR<IngressDetail>(path, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [draft, setDraft] = useState<IngressRule[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [problem, setProblem] = useState("")

  if (!detail) return <div className="card"><LoadingState /></div>
  // 反映中と削除中は、編集も削除もできない(サーバーも 409)。
  const locked = isBusyStatus(detail.status)
  const saved = detail.pending_rules ?? detail.rules
  const rows = draft ?? saved
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(saved)
  const firstChoice = choices[0]

  /**
   * 下書きの行を 1 つ書き換える。
   *
   * @param index 行の番号。
   * @param updates 変える項目。
   */
  function updateRow(index: number, updates: Partial<IngressRule>) {
    setDraft(rows.map((rule, at) => (at === index ? { ...rule, ...updates } : rule)))
  }

  /** 下書きを保存する(全件を送って、未適用の変更にする)。 */
  async function save() {
    // path は「/」で始まり、転送先が決まっていること。
    if (rows.some((rule) => !rule.path.startsWith("/") || !rule.serviceId || !(rule.port >= 1 && rule.port <= 65535))) {
      setProblem("path は「/」から始め、Service とポートを指定してください。")
      return
    }
    setProblem("")
    await run(async () => {
      await api.put(path, { rules: rows })
      setDraft(null)
      notify("ルールを保存しました(未適用)")
    })
  }

  /**
   * 操作を実行し、終わったら取り直す(失敗は通知する)。
   *
   * @param action 実行する処理。
   */
  async function run(action: () => Promise<void>) {
    setBusy(true)
    try {
      await action()
      await mutate()
    } catch (caught) {
      notify(caught instanceof Error ? caught.message : "操作に失敗しました", true)
    } finally {
      setBusy(false)
    }
  }

  const pendingLines = detail.pending_rules ? diffRules(detail.rules, detail.pending_rules) : []
  return (
    <div className="card">
      {/* ホスト、状態、操作。 */}
      <div className="row pad" style={{ paddingBottom: 10, flexWrap: "nowrap" }}>
        <div className="grow" style={{ minWidth: 0 }}>
          <a className="mono" style={{ fontWeight: 500, fontSize: 14, color: "var(--accent)" }} href={`https://${detail.host}`} target="_blank" rel="noreferrer">https://{detail.host}</a>
          <span className="faint mono" style={{ marginLeft: 10, fontSize: 11.5 }}>{detail.ingressId}</span>
        </div>
        <Badge status={detail.status} />
        <button className="btn sm" type="button" disabled={locked || busy} onClick={() => setDraft([...rows, { path: "/", serviceId: firstChoice?.serviceId ?? "", port: firstChoice?.ports[0] ?? 80 }])}>+ ルール追加</button>
        <button className="btn sm danger" type="button" disabled={locked || busy} onClick={() => setConfirming(true)}>削除</button>
      </div>
      {/* ルールの行。 */}
      <div className="tscroll"><div className="tinner" style={{ minWidth: 680 }}>
        {rows.length === 0 && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>ルールがありません。</div>}
        {rows.map((rule, index) => {
          const isNew = !detail.rules.some((current) => current.path === rule.path)
          const isChanged = !isNew && detail.rules.some((current) => current.path === rule.path && (current.serviceId !== rule.serviceId || current.port !== rule.port))
          const state = isNew ? "new" : isChanged ? "chg" : ""
          const known = choices.some((choice) => choice.serviceId === rule.serviceId)
          return (
            <div className="tr" key={index} style={{ ["--cols" as string]: "1.1fr 20px 1.4fr 110px 40px" }}>
              <input className={`inp mono ${state}`} value={rule.path} onChange={(event) => updateRow(index, { path: event.target.value })} disabled={locked} aria-label="パス" placeholder="/" />
              <span className="faint">→</span>
              <select className={`inp mono ${state}`} value={rule.serviceId} onChange={(event) => updateRow(index, { serviceId: event.target.value, port: choices.find((choice) => choice.serviceId === event.target.value)?.ports[0] ?? rule.port })} disabled={locked} aria-label="Service">
                {!known && <option value={rule.serviceId}>{rule.serviceId || "(選んでください)"}</option>}
                {choices.map((choice) => <option key={choice.serviceId} value={choice.serviceId}>{choice.label}</option>)}
              </select>
              <input className={`inp mono ${state}`} type="number" value={Number.isNaN(rule.port) ? "" : rule.port} onChange={(event) => updateRow(index, { port: event.target.value === "" ? Number.NaN : Number(event.target.value) })} disabled={locked} aria-label="ポート" />
              <button className="btn ico ghost" type="button" disabled={locked} onClick={() => setDraft(rows.filter((_, at) => at !== index))} aria-label="ルールを削除">×</button>
            </div>
          )
        })}
      </div></div>
      {problem && <div style={{ color: "var(--bad)", fontSize: 12.5, padding: "0 18px 8px" }}>{problem}</div>}
      {dirty && <div className="row" style={{ padding: "0 18px 14px" }}><button className="btn sm primary" type="button" disabled={busy || locked} onClick={() => void save()}>保存</button><button className="btn sm" type="button" disabled={busy} onClick={() => { setDraft(null); setProblem("") }}>編集を戻す</button></div>}
      {/* 反映待ち。 */}
      {detail.pending_rules && !dirty && (
        <div style={{ margin: "4px 18px 18px", display: "flex", flexDirection: "column", gap: 10 }}>
          <DiffBox lines={pendingLines} />
          <ApplyBar title="反映待ちの変更" busy={busy || locked} onDiscard={() => void run(async () => { await api.post(`${path}/discard`); notify("破棄しました") })} onApply={() => void run(async () => { await api.post(`${path}/apply`); notify("反映を開始しました") })} />
        </div>
      )}
      {confirming && (
        <Modal title="Ingress を削除しますか?" description={detail.host} onClose={() => setConfirming(false)} footer={<><button className="btn" type="button" onClick={() => setConfirming(false)}>キャンセル</button><button className="btn warn" type="button" disabled={busy} onClick={() => void run(async () => { await api.delete(path); setConfirming(false); notify("削除を開始しました"); onChanged() })}>削除</button></>}>
          <div className="mut">ホスト名とルールが削除されます。</div>
        </Modal>
      )}
    </div>
  )
}

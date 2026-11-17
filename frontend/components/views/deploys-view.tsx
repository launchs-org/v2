"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { DeployTab, ProjectViewProps } from "@/lib/nav"
import type { DeployDetail, DeployListResponse, ServicePort, VolumeListResponse } from "@/lib/types"
import { ApplyBar, Badge, DiffBox, ErrorState, isBusyStatus, LoadingState, Modal, ToastNotice } from "@/components/ui-kit"
import { LogTail } from "@/components/log-tail"

type DeploysViewProps = ProjectViewProps & { deployId: string; tab: DeployTab; onTabChange: (tab: DeployTab) => void }
type DiffLine = { cls: "d-add" | "d-chg" | "d-del"; text: string }

const TABS: { key: DeployTab; label: string }[] = [
  { key: "overview", label: "概要" }, { key: "config", label: "起動設定" }, { key: "mounts", label: "マウント" }, { key: "service", label: "Service" }, { key: "logs", label: "ログ" },
]
const MAX_REPLICAS = 5
const PORT_NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/

/**
 * 日時(ISO)を、画面に出す短い形にする。
 *
 * @param iso API の日時。
 * @returns 「2026/11/16 16:32」の形。読めなければ、そのまま。
 */
function formatAt(iso: string): string {
  // 読めない値は、そのまま見せる。
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString("ja-JP", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", timeZone: "Asia/Tokyo" })
}

/**
 * ポート一覧を、検査する。
 *
 * @param ports ポートの一覧。
 * @returns 問題の説明。合っていれば空文字。
 */
function validatePorts(ports: ServicePort[]): string {
  // 1つ以上、20個まで。
  if (ports.length === 0) return "ポートを1つ以上入れてください。"
  if (ports.length > 20) return "ポートは20個までです。"
  const names = new Set<string>()
  const pairs = new Set<string>()
  for (const port of ports) {
    // 名前の規則、番号の範囲、重複を調べる。
    if (!port.name || port.name.length > 15 || !PORT_NAME.test(port.name) || !/[a-z]/.test(port.name)) return `ポート名「${port.name}」は、英小文字・数字・ハイフンで15文字まで(英字を1つ以上)にしてください。`
    if (!Number.isInteger(port.port) || port.port < 1 || port.port > 65535) return "ポート番号は 1〜65535 にしてください。"
    if (names.has(port.name)) return `ポート名「${port.name}」が重複しています。`
    const pair = `${port.protocol}/${port.port}`
    if (pairs.has(pair)) return `${port.protocol} の ${port.port} 番が重複しています。`
    names.add(port.name)
    pairs.add(pair)
  }
  return ""
}

/**
 * 2つの一覧を、キーで比べて、差分の行にする。
 *
 * @param before いまの値。
 * @param after 変更後の値。
 * @returns + 追加、~ 変更、- 削除の行。
 */
function diffByKey(before: Map<string, string>, after: Map<string, string>): DiffLine[] {
  const lines: DiffLine[] = []
  // 追加と変更を見つける。
  for (const [key, value] of after) {
    if (!before.has(key)) lines.push({ cls: "d-add", text: `+ ${key} = ${value}` })
    else if (before.get(key) !== value) lines.push({ cls: "d-chg", text: `~ ${key}: ${before.get(key)}   →   ${value}` })
  }
  // 削除を見つける。
  for (const [key, value] of before) if (!after.has(key)) lines.push({ cls: "d-del", text: `- ${key} = ${value}` })
  return lines
}

/**
 * デプロイの画面。deployId が空なら一覧、あれば詳細(タブつき)。上のバーの「作成」で、作成のダイアログを開く。
 *
 * @param props 共通の引数と、開いている deploy、タブ。
 */
export function DeploysView(props: DeploysViewProps) {
  const { project, refreshInterval, nav, search, createTick } = props
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)
  const [creating, setCreating] = useState(false)
  const [lastTick, setLastTick] = useState(createTick)

  // 上のバーの「作成」が押されたら(回数が増えたら)、ダイアログを開く。
  useEffect(() => {
    if (createTick !== lastTick) {
      setLastTick(createTick)
      if (!props.deployId) setCreating(true)
    }
  }, [createTick, lastTick, props.deployId])

  /**
   * 通知を出す。
   *
   * @param message 文。
   * @param error 失敗なら true。
   */
  function notify(message: string, error = false) { setToast({ message, error }) }

  return (
    <>
      {props.deployId ? <DeployDetailView {...props} notify={notify} /> : <DeployList project={project} refreshInterval={refreshInterval} nav={nav} search={search} />}
      {creating && <CreateDeployModal projectId={project.projectId} onClose={() => setCreating(false)} onCreated={(id) => { setCreating(false); nav.openDeploy(id) }} notify={notify} />}
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => setToast(null)} />}
    </>
  )
}

/** デプロイの一覧(表)。 */
function DeployList({ project, refreshInterval, nav, search }: Pick<ProjectViewProps, "project" | "refreshInterval" | "nav" | "search">) {
  const { data, error, mutate } = useSWR<DeployListResponse>(`/api/v1/projects/${encodeURIComponent(project.projectId)}/deploys?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const query = search.trim().toLowerCase()
  const rows = (data?.deploys ?? []).filter((deploy) => !query || deploy.deployId.toLowerCase().includes(query) || deploy.imageRef.toLowerCase().includes(query))
  const cols = "120px 2fr 90px 90px 140px 150px"
  return (
    <>
      <div><h1 className="h1 disp">デプロイ</h1></div>
      {error && <ErrorState message="デプロイ一覧を取得できませんでした。" onRetry={() => void mutate()} />}
      {!data && !error && <LoadingState />}
      {data && (
        <div className="card tscroll"><div className="tinner" style={{ minWidth: 860 }}>
          <div className="tr th" style={{ "--cols": cols } as React.CSSProperties}><span>デプロイ</span><span>イメージ</span><span>スペック</span><span>レプリカ</span><span>状態</span><span>作成日時</span></div>
          {rows.map((deploy) => (
            <button key={deploy.deployId} type="button" className="tr rowb" style={{ "--cols": cols } as React.CSSProperties} onClick={() => nav.openDeploy(deploy.deployId)}>
              <span className="mono" style={{ fontWeight: 500, color: "var(--accent)" }} title={deploy.deployId}>{deploy.deployId.replace("deploy-", "").slice(0, 8)}</span>
              <span className="mono mut" style={{ wordBreak: "break-all" }}>{deploy.imageRef}</span>
              <span className="mono">{deploy.spec}</span>
              <span className="mono">{deploy.replicas}</span>
              <span><Badge status={deploy.status} /></span>
              <span className="faint mono">{formatAt(deploy.createdAt)}</span>
            </button>
          ))}
          {rows.length === 0 && <div className="tr mut" style={{ "--cols": "1fr" } as React.CSSProperties}>{data.deploys.length ? "一致するデプロイがありません。" : "デプロイはまだありません。上の「+ デプロイを作成」から作れます。"}</div>}
        </div></div>
      )}
    </>
  )
}

/** デプロイの作成ダイアログ。 */
function CreateDeployModal({ projectId, onClose, onCreated, notify }: { projectId: string; onClose: () => void; onCreated: (deployId: string) => void; notify: (message: string, error?: boolean) => void }) {
  const [imageRef, setImageRef] = useState("")
  const [spec, setSpec] = useState("small")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  /** 作成を頼む。成功したら、その deploy の詳細を開く。 */
  async function submit(event: React.FormEvent) {
    event.preventDefault()
    // 手元で検査してから送る。
    const trimmed = imageRef.trim()
    if (!trimmed || /\s/.test(trimmed)) { setError("イメージを、空白なしで入力してください。"); return }
    setBusy(true)
    setError("")
    try {
      const result = await api.post<{ deployId: string }>("/api/v1/deploy", { projectId, imageRef: trimmed, spec })
      notify("デプロイを作成しました")
      onCreated(result.deployId)
    } catch (caught) { setError(caught instanceof Error ? caught.message : "作成できませんでした") } finally { setBusy(false) }
  }

  return (
    <Modal title="デプロイを作成" onClose={onClose} footer={<><button className="btn" type="button" onClick={onClose}>キャンセル</button><button className="btn primary" type="submit" form="deploy-create" disabled={busy}>{busy ? "作成中…" : "作成"}</button></>}>
      <form id="deploy-create" style={{ display: "flex", flexDirection: "column", gap: 14 }} onSubmit={submit}>
        <label className="lbl">イメージ<input className="inp mono" value={imageRef} onChange={(event) => setImageRef(event.target.value)} placeholder="nginx:alpine" autoFocus /></label>
        <label className="lbl">スペック<select className="inp" value={spec} onChange={(event) => setSpec(event.target.value)}><option value="small">small</option><option value="medium">medium</option><option value="large">large</option></select></label>
        {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{error}</div>}
      </form>
    </Modal>
  )
}

/** デプロイの詳細(見出し、タブ、各タブの中身)。 */
function DeployDetailView({ deployId, tab, onTabChange, project, refreshInterval, nav, notify }: DeploysViewProps & { notify: (message: string, error?: boolean) => void }) {
  const path = `/api/v1/deploy/${encodeURIComponent(deployId)}`
  const { data, error, mutate } = useSWR<DeployDetail>(path, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  /**
   * 操作を1つ実行して、結果を通知し、取り直す。
   *
   * @param action 実行する非同期処理。
   * @param done 成功の通知。
   */
  async function run(action: () => Promise<unknown>, done: string) {
    setBusy(true)
    try { await action(); notify(done); await mutate() } catch (caught) { notify(caught instanceof Error ? caught.message : "操作を完了できませんでした", true) } finally { setBusy(false) }
  }

  if (error && !data) return <><button className="btn" type="button" onClick={nav.closeDeploy}>一覧へ</button><ErrorState message="デプロイを取得できませんでした(削除されたかもしれません)。" onRetry={() => void mutate()} /></>
  if (!data) return <LoadingState />
  // 反映中・起動中・削除中は、編集も削除もできない。
  const locked = isBusyStatus(data.status)
  const config = data.config
  const hasConfigPending = config.pending_env !== null || config.pending_command !== null || config.pending_args !== null
  const hasMountsPending = data.volumes.pending_mounts !== null
  const hasServicePending = Boolean(data.service?.pending_ports)
  const hasPending = hasConfigPending || hasMountsPending || hasServicePending
  const pendingTabs: Record<DeployTab, boolean> = { overview: false, config: hasConfigPending, mounts: hasMountsPending, service: hasServicePending, logs: false }
  const lockedMessage = data.status === "applying" ? "反映中です。完了するまで編集・削除はできません。" : data.status === "pending" ? "起動中です。完了するまで編集はできません。" : data.status === "deleting" ? "削除処理中です。" : ""

  return (
    <>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="grow">
          <div className="row"><h1 className="h1 disp mono" style={{ fontSize: 24 }}>{data.deployId}</h1><Badge status={data.status} /><span className="badge b-dim">{data.spec}</span></div>
          <div className="mono mut" style={{ marginTop: 6, wordBreak: "break-all" }}>{data.imageRef}</div>
        </div>
        <button className="btn" type="button" onClick={nav.closeDeploy}>一覧へ</button>
        <button className="btn danger" type="button" onClick={() => setConfirmDelete(true)} disabled={locked || busy}>削除</button>
      </div>
      {lockedMessage && <div className="badge b-warn" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>{lockedMessage}</div>}
      <div className="tabs" role="tablist">
        {TABS.map((item) => <button key={item.key} type="button" role="tab" aria-selected={tab === item.key} className={`tab ${tab === item.key ? "on" : ""}`} onClick={() => onTabChange(item.key)}>{item.label}{pendingTabs[item.key] && <span className="pdot" />}</button>)}
      </div>
      {tab === "overview" && <OverviewTab data={data} locked={locked} busy={busy} hasPending={hasPending} run={run} onTabChange={onTabChange} />}
      {tab === "config" && <ConfigTab data={data} locked={locked} busy={busy} run={run} />}
      {tab === "mounts" && <MountsTab data={data} projectId={project.projectId} locked={locked} busy={busy} run={run} />}
      {tab === "service" && <ServiceTab data={data} locked={locked} busy={busy} run={run} />}
      {tab === "logs" && <LogsTab data={data} refreshInterval={refreshInterval} />}
      {confirmDelete && (
        <Modal title="デプロイを削除しますか?" description="デプロイとログ、Service を削除します。ボリュームは残ります。元に戻せません。" onClose={() => setConfirmDelete(false)} footer={<><button className="btn" type="button" onClick={() => setConfirmDelete(false)}>キャンセル</button><button className="btn warn" type="button" disabled={busy} onClick={() => void run(async () => { await api.delete(path); setConfirmDelete(false); nav.closeDeploy() }, "削除を受け付けました")}>削除する</button></>}><div className="mono">{data.deployId}</div></Modal>
      )}
    </>
  )
}

type TabProps = { data: DeployDetail; locked: boolean; busy: boolean; run: (action: () => Promise<unknown>, done: string) => Promise<void> }

/** 概要タブ: スケール、Pod、詳細、未反映の知らせ。 */
function OverviewTab({ data, locked, busy, hasPending, run, onTabChange }: TabProps & { hasPending: boolean; onTabChange: (tab: DeployTab) => void }) {
  const path = `/api/v1/deploy/${encodeURIComponent(data.deployId)}`
  const [wanted, setWanted] = useState(data.replicas)
  // サーバーの台数が変わったら(反映が終わったら)、下書きも合わせる。
  useEffect(() => { setWanted(data.replicas) }, [data.replicas])
  const scaleLocked = locked || data.status === "pending"
  const changed = wanted !== data.replicas
  const mountCount = (data.volumes.pending_mounts ?? data.volumes.mounts).length
  return (
    <>
      <div className="grid g2">
        <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <h2 className="h2">スケール</h2>
          <div className="row" style={{ gap: 18 }}>
            <button className="btn ico" type="button" aria-label="レプリカを減らす" onClick={() => setWanted((count) => count - 1)} disabled={scaleLocked || wanted <= 0}>−</button>
            <div style={{ textAlign: "center", minWidth: 70 }}><div className="disp" style={{ fontSize: 44, fontWeight: 500, lineHeight: 1 }}>{wanted}</div><div className="faint" style={{ fontSize: 12 }}>レプリカ</div></div>
            <button className="btn ico" type="button" aria-label="レプリカを増やす" onClick={() => setWanted((count) => count + 1)} disabled={scaleLocked || wanted >= MAX_REPLICAS}>＋</button>
          </div>
          {changed ? (
            <div className="applybar" style={{ padding: "10px 12px" }}><span className="pdot" /><div className="grow" style={{ fontSize: 13 }}><b style={{ fontWeight: 500 }}>{data.replicas} → {wanted} に変更</b></div><button className="btn sm" type="button" onClick={() => setWanted(data.replicas)} disabled={busy}>破棄</button><button className="btn sm warn" type="button" disabled={busy || scaleLocked} onClick={() => void run(() => api.post(`${path}/scale`, { replicas: wanted }), "スケールを受け付けました")}>反映</button></div>
          ) : <div className="mut" style={{ fontSize: 12.5 }}>0〜{MAX_REPLICAS} 台</div>}
        </div>
        <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <h2 className="h2">Pods</h2>
          {data.pods.map((pod) => <button key={pod.podId} type="button" className="opt" onClick={() => onTabChange("logs")}><span className="mono" style={{ color: "var(--text)" }}>{pod.podId}</span><span className="grow" /><Badge status={pod.status} /><span className="faint" style={{ fontSize: 12, whiteSpace: "nowrap" }}>ログ ›</span></button>)}
          {data.pods.length === 0 && <div className="mut">稼働中の Pod はありません。</div>}
        </div>
      </div>
      <div className="card pad"><dl className="kv" style={{ margin: 0 }}>
        <div><dt>Project</dt><dd className="mono">{data.projectId}</dd></div>
        <div><dt>Created</dt><dd className="mono">{formatAt(data.createdAt)}</dd></div>
        <div><dt>Env vars</dt><dd>{Object.keys(data.config.pending_env ?? data.config.env).length} 件</dd></div>
        <div><dt>Mounts</dt><dd>{mountCount} 件</dd></div>
        <div><dt>Service</dt><dd className="mono">{data.service ? data.service.serviceId : "なし"}</dd></div>
      </dl></div>
      {hasPending && <div className="applybar"><span className="pdot" /><div className="grow"><b>このデプロイに未反映の変更があります</b><div className="mut" style={{ fontSize: 13 }}>起動設定・マウント・Service のタブの、青い点を確認してください。</div></div></div>}
    </>
  )
}

/** 起動設定タブ: 環境変数、Command、Args。下書き → 保存(pending)→ 反映。 */
function ConfigTab({ data, locked, busy, run }: TabProps) {
  const path = `/api/v1/deploy/${encodeURIComponent(data.deployId)}`
  const config = data.config
  const hasPending = config.pending_env !== null || config.pending_command !== null || config.pending_args !== null
  // 編集の元は、未反映の変更があればそれ、無ければ、いまの値。
  const sourceEnv = config.pending_env ?? config.env
  const sourceCommand = hasPending ? config.pending_command : config.command
  const sourceArgs = hasPending ? config.pending_args : config.args
  const [rows, setRows] = useState<[string, string][]>(Object.entries(sourceEnv))
  const [command, setCommand] = useState(sourceCommand ?? "")
  const [args, setArgs] = useState((sourceArgs ?? []).join(" "))
  const [problem, setProblem] = useState("")
  // サーバーの値が変わったら(保存・反映・破棄の後)、下書きを、最新の値に合わせる。
  const sourceKey = JSON.stringify([sourceEnv, sourceCommand, sourceArgs])
  useEffect(() => {
    setRows(Object.entries(sourceEnv))
    setCommand(sourceCommand ?? "")
    setArgs((sourceArgs ?? []).join(" "))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKey])

  /** 下書きを検査して、pending として保存する。 */
  function save() {
    // キーの空と重複を断る。
    const keys = rows.map(([key]) => key.trim()).filter(Boolean)
    if (new Set(keys).size !== keys.length) { setProblem("環境変数のキーが重複しています。"); return }
    if (rows.some(([key, value]) => !key.trim() && value)) { setProblem("キーが空の行があります。"); return }
    setProblem("")
    const env: Record<string, string> = {}
    for (const [key, value] of rows) if (key.trim()) env[key.trim()] = value
    const argList = args.trim() ? args.trim().split(/\s+/) : null
    void run(() => api.put(`${path}/config`, { env, command: command.trim() || null, args: argList }), "起動設定を保存しました(未反映)")
  }

  // 差分: いまの値と、保存済みの未反映の値。
  const diff: DiffLine[] = hasPending ? diffByKey(new Map(Object.entries(config.env)), new Map(Object.entries(config.pending_env ?? config.env))) : []
  if (hasPending && config.pending_command !== config.command) diff.push({ cls: "d-chg", text: `~ command: ${config.command ?? "(default)"}   →   ${config.pending_command ?? "(default)"}` })
  if (hasPending && JSON.stringify(config.pending_args) !== JSON.stringify(config.args)) diff.push({ cls: "d-chg", text: `~ args: ${(config.args ?? []).join(" ") || "(none)"}   →   ${(config.pending_args ?? []).join(" ") || "(none)"}` })

  return (
    <div className="split">
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <div className="row"><h2 className="h2 grow">環境変数</h2><button className="btn sm" type="button" onClick={() => setRows([...rows, ["", ""]])} disabled={locked}>+ 追加</button></div>
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {rows.map(([key, value], index) => (
            <div key={index} className="row" style={{ flexWrap: "nowrap", gap: 8 }}>
              <input className="inp mono" style={{ flex: "1 1 30%", minWidth: 0 }} value={key} onChange={(event) => setRows(rows.map((row, at) => at === index ? [event.target.value, row[1]] : row))} disabled={locked} aria-label="キー" />
              <span className="faint">=</span>
              <input className="inp mono" style={{ flex: "2 1 50%", minWidth: 0 }} value={value} onChange={(event) => setRows(rows.map((row, at) => at === index ? [row[0], event.target.value] : row))} disabled={locked} aria-label="値" />
              <button className="btn ico ghost" type="button" onClick={() => setRows(rows.filter((_, at) => at !== index))} disabled={locked} aria-label="削除">×</button>
            </div>
          ))}
        </div>
        <div className="grid g2">
          <label className="lbl">Command<input className="inp mono" value={command} onChange={(event) => setCommand(event.target.value)} disabled={locked} placeholder="イメージのデフォルト" /></label>
          <label className="lbl">Args<input className="inp mono" value={args} onChange={(event) => setArgs(event.target.value)} disabled={locked} placeholder="スペース区切り" /></label>
        </div>
        {problem && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{problem}</div>}
        <button className="btn primary" type="button" style={{ alignSelf: "flex-start" }} onClick={save} disabled={locked || busy}>保存</button>
      </div>
      <div className="col">
        {hasPending ? (
          <>
            <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}><h2 className="h2">変更内容の確認</h2><DiffBox lines={diff} /></div>
            <ApplyBar title="起動設定に未反映の変更があります" hint="反映すると Pod が再作成されます。" busy={locked || busy} onDiscard={() => void run(() => api.post(`${path}/config/discard`), "変更を破棄しました")} onApply={() => void run(() => api.post(`${path}/config/apply`), "反映を開始しました")} />
          </>
        ) : <div className="card pad mut">変更はありません。左で編集して保存すると、ここに変更内容が表示されます。</div>}
      </div>
    </div>
  )
}

/** マウントタブ。 */
function MountsTab({ data, projectId, locked, busy, run }: TabProps & { projectId: string }) {
  const path = `/api/v1/deploy/${encodeURIComponent(data.deployId)}`
  const { data: volumeData } = useSWR<VolumeListResponse>(`/api/v1/projects/${encodeURIComponent(projectId)}/volumes?limit=100`, authenticatedFetcher)
  const volumes = volumeData?.volumes ?? []
  const pending = data.volumes.pending_mounts
  const source = pending ?? data.volumes.mounts
  const [rows, setRows] = useState(source.map((mount) => ({ ...mount })))
  const [problem, setProblem] = useState("")
  const sourceKey = JSON.stringify(source)
  useEffect(() => { setRows(source.map((mount) => ({ ...mount }))) // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKey])

  /** 下書きを検査して、pending として保存する。 */
  function save() {
    // volume の未選択、パスの形、重複を断る。
    if (rows.length > 10) { setProblem("マウントは10件までです。"); return }
    for (const row of rows) {
      if (!row.volumeId) { setProblem("volume を選んでください。"); return }
      if (!/^\/[A-Za-z0-9._\-/]*$/.test(row.mountPath) || row.mountPath === "/" || row.mountPath.includes("..") || row.mountPath.length > 200) { setProblem(`パス「${row.mountPath}」は、「/」で始まる英数字と . _ - / のパスにしてください。`); return }
    }
    if (new Set(rows.map((row) => row.volumeId)).size !== rows.length || new Set(rows.map((row) => row.mountPath)).size !== rows.length) { setProblem("同じ volume・同じパスは、重ねて使えません。"); return }
    setProblem("")
    void run(() => api.put(`${path}/mounts`, { mounts: rows }), "マウントを保存しました(未反映)")
  }

  const nameOf = (id: string) => volumes.find((volume) => volume.volumeId === id)?.name ?? id
  const before = new Map(data.volumes.mounts.map((mount) => [mount.mountPath, nameOf(mount.volumeId)]))
  const after = new Map((pending ?? []).map((mount) => [mount.mountPath, nameOf(mount.volumeId)]))
  const diff = pending ? diffByKey(before, after) : []

  return (
    <div className="split">
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <div className="row"><h2 className="h2 grow">ボリュームマウント</h2><button className="btn sm" type="button" onClick={() => setRows([...rows, { volumeId: "", mountPath: "" }])} disabled={locked}>+ 追加</button></div>
        {rows.length === 0 && <div className="mut">マウントはありません。</div>}
        {rows.map((row, index) => (
          <div key={index} className="row" style={{ flexWrap: "nowrap", gap: 8 }}>
            <select className="inp mono" style={{ flex: "1 1 40%", minWidth: 0 }} value={row.volumeId} onChange={(event) => setRows(rows.map((item, at) => at === index ? { ...item, volumeId: event.target.value } : item))} disabled={locked} aria-label="ボリューム">
              <option value="">volume を選ぶ</option>
              {volumes.map((volume) => <option key={volume.volumeId} value={volume.volumeId}>{volume.name}</option>)}
              {row.volumeId && !volumes.some((volume) => volume.volumeId === row.volumeId) && <option value={row.volumeId}>{row.volumeId}</option>}
            </select>
            <span className="faint">→</span>
            <input className="inp mono" style={{ flex: "1 1 50%", minWidth: 0 }} value={row.mountPath} onChange={(event) => setRows(rows.map((item, at) => at === index ? { ...item, mountPath: event.target.value } : item))} disabled={locked} aria-label="マウントパス" placeholder="/data" />
            <button className="btn ico ghost" type="button" onClick={() => setRows(rows.filter((_, at) => at !== index))} disabled={locked} aria-label="削除">×</button>
          </div>
        ))}
        {problem && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{problem}</div>}
        <button className="btn primary" type="button" style={{ alignSelf: "flex-start" }} onClick={save} disabled={locked || busy}>保存</button>
        <div className="faint" style={{ fontSize: 12.5 }}>デプロイを削除してもボリュームは残ります。</div>
      </div>
      <div className="col">
        {pending ? (
          <>
            <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}><h2 className="h2">変更内容の確認</h2><DiffBox lines={diff.length ? diff : [{ cls: "d-chg", text: "(マウントを全て外す)" }]} /></div>
            <ApplyBar title="マウントに未反映の変更があります" hint="反映すると Pod が再作成されます。" busy={locked || busy} onDiscard={() => void run(() => api.post(`${path}/mounts/discard`), "変更を破棄しました")} onApply={() => void run(() => api.post(`${path}/mounts/apply`), "反映を開始しました")} />
          </>
        ) : <div className="card pad mut">変更はありません。左で編集して保存すると、ここに変更内容が表示されます。</div>}
      </div>
    </div>
  )
}

/** Service タブ。 */
function ServiceTab({ data, locked, busy, run }: TabProps) {
  const path = `/api/v1/deploy/${encodeURIComponent(data.deployId)}`
  const service = data.service
  const pending = service?.pending_ports ?? null
  const source: ServicePort[] = pending ?? service?.ports ?? [{ name: "web", port: 8080, protocol: "tcp" }]
  const [rows, setRows] = useState<ServicePort[]>(source.map((port) => ({ ...port })))
  const [problem, setProblem] = useState("")
  const [confirmDelete, setConfirmDelete] = useState(false)
  const sourceKey = JSON.stringify(source)
  useEffect(() => { setRows(source.map((port) => ({ ...port }))) // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKey])
  // ポートを変えられるのは、Service が稼働中のときだけ。
  const editable = !locked && (!service || service.status === "active")

  /** 検査して、作成か、pending への保存を頼む。 */
  function save() {
    const message = validatePorts(rows)
    setProblem(message)
    if (message) return
    if (!service) void run(() => api.post(`${path}/service`, { ports: rows }), "Service を作成しました")
    else void run(() => api.patch(`${path}/service`, { ports: rows }), "ポートを保存しました(未反映)")
  }

  const diff = service && pending ? diffByKey(new Map(service.ports.map((port) => [port.name, `${port.port}/${port.protocol}`])), new Map(pending.map((port) => [port.name, `${port.port}/${port.protocol}`]))) : []

  const editor = (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div className="row"><h2 className="h2 grow">公開ポート</h2><button className="btn sm" type="button" onClick={() => setRows([...rows, { name: "", port: 8080, protocol: "tcp" }])} disabled={!editable}>+ 追加</button></div>
      {rows.map((row, index) => (
        <div key={index} className="row" style={{ flexWrap: "nowrap", gap: 8 }}>
          <input className="inp mono" style={{ flex: "1 1 34%", minWidth: 0 }} value={row.name} onChange={(event) => setRows(rows.map((item, at) => at === index ? { ...item, name: event.target.value } : item))} disabled={!editable} aria-label="ポート名" placeholder="http" />
          <input className="inp mono" style={{ flex: "1 1 26%", minWidth: 0 }} type="number" value={Number.isNaN(row.port) ? "" : row.port} onChange={(event) => setRows(rows.map((item, at) => at === index ? { ...item, port: event.target.value === "" ? Number.NaN : Number(event.target.value) } : item))} disabled={!editable} aria-label="ポート番号" />
          <select className="inp mono" style={{ flex: "0 0 90px" }} value={row.protocol} onChange={(event) => setRows(rows.map((item, at) => at === index ? { ...item, protocol: event.target.value as "tcp" | "udp" } : item))} disabled={!editable} aria-label="プロトコル"><option value="tcp">tcp</option><option value="udp">udp</option></select>
          <button className="btn ico ghost" type="button" onClick={() => setRows(rows.filter((_, at) => at !== index))} disabled={!editable} aria-label="削除">×</button>
        </div>
      ))}
      {problem && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{problem}</div>}
      <button className="btn primary" type="button" style={{ alignSelf: "flex-start" }} onClick={save} disabled={!editable || busy}>{service ? "保存" : "Service を作成"}</button>
      <div className="faint" style={{ fontSize: 12.5 }}>ポート名は英小文字・数字・ハイフンで 15 文字まで。最低 1 ポートが必要です。</div>
    </div>
  )

  if (!service) return <div className="split"><div className="col"><div className="card pad"><h2 className="h2">Service が未作成です</h2><div className="mut">ポートを公開すると、クラスタ内 DNS 名と ClusterIP が払い出されます。</div></div>{editor}</div></div>
  return (
    <>
      <div className="split">
        <div className="col">
          <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 16 }}>
            <div className="row"><h2 className="h2 grow mono">{service.serviceId}</h2><Badge status={service.status} /></div>
            <dl className="kv" style={{ margin: 0 }}><div><dt>DNS</dt><dd className="mono">{service.dnsName ?? "(払い出し中)"}</dd></div><div><dt>ClusterIP</dt><dd className="mono">{service.clusterIP ?? "(払い出し中)"}</dd></div></dl>
            <button className="btn danger" type="button" style={{ alignSelf: "flex-start" }} onClick={() => setConfirmDelete(true)} disabled={locked || busy || service.status === "pending" || service.status === "deleting"}>Service を削除</button>
          </div>
          {editor}
        </div>
        <div className="col">
          {pending ? (
            <>
              <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}><h2 className="h2">変更内容の確認</h2><DiffBox lines={diff} /></div>
              <ApplyBar title="Service ポートに未反映の変更があります" hint="Pod は再起動しません。" busy={!editable || busy} onDiscard={() => void run(() => api.post(`${path}/service/discard`), "変更を破棄しました")} onApply={() => void run(() => api.post(`${path}/service/apply`), "反映を開始しました")} />
            </>
          ) : <div className="card pad mut">変更はありません。左で編集して保存すると、ここに変更内容が表示されます。</div>}
        </div>
      </div>
      {confirmDelete && (
        <Modal title="Service を削除しますか?" description="この Service を参照している Ingress のルールも、削除されます。" onClose={() => setConfirmDelete(false)} footer={<><button className="btn" type="button" onClick={() => setConfirmDelete(false)}>キャンセル</button><button className="btn warn" type="button" disabled={busy} onClick={() => void run(async () => { await api.delete(`${path}/service`); setConfirmDelete(false) }, "削除を受け付けました")}>削除する</button></>}><div className="mono">{service.serviceId}</div></Modal>
      )}
    </>
  )
}

/** ログタブ: Pod のチップと、ログ。 */
function LogsTab({ data, refreshInterval }: { data: DeployDetail; refreshInterval: number }) {
  // 稼働中の Pod を先に、新しい順。
  const pods = [...data.logPods].sort((first, second) => Number(second.running) - Number(first.running) || second.startedAt.localeCompare(first.startedAt))
  const [picked, setPicked] = useState("")
  const podId = pods.some((pod) => pod.podId === picked) ? picked : pods[0]?.podId ?? ""
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div className="row">
        <span className="faint" style={{ width: 36 }}>Pod</span>
        {pods.map((pod) => <button key={pod.podId} type="button" className={`chip ${pod.podId === podId ? "on" : ""}`} onClick={() => setPicked(pod.podId)}>{pod.podId.replace("pod-", "").slice(0, 8)}{pod.running ? "" : "(終了)"}</button>)}
        {pods.length === 0 && <span className="mut">ログのある Pod がありません</span>}
      </div>
      <LogTail key={`${data.deployId}:${podId}`} deployId={data.deployId} pods={pods} podId={podId} refreshInterval={refreshInterval} />
    </div>
  )
}

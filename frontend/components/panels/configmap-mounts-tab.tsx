"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { revalidateProject } from "@/lib/revalidate"
import { MAX_MOUNTS, validateKey, validateMode, validateMountPath } from "@/lib/configmap-files"
import type { Nav } from "@/lib/nav"
import type { ConfigMapDetail, ConfigMapListResponse, ConfigMapMountsState } from "@/lib/types"
import { ApplyBar, DiffBox } from "@/components/ui-kit"

/** 画面で編集している、マウント 1 つ(下書き)。revision が 0 のときは、保存するとき、サーバーが版を決める。 */
type Row = { configMapId: string; key: string; path: string; mode: string; revision: number }

/** 権限(mode)の選択肢。ファイルは、読み取り専用でマウントされるので、「書き込み」の権限は、コンテナの中で、実際には使えない(所有者の表示としてだけ意味がある)。 */
const MODE_PRESETS: { mode: string; label: string }[] = [
  { mode: "0644", label: "標準(0644)" },
  { mode: "0444", label: "読み取り専用(0444)" },
  { mode: "0400", label: "所有者だけ読み取り(0400)" },
  { mode: "0600", label: "所有者だけ読み書き(0600)" },
  { mode: "0755", label: "実行できる(0755)" },
]
/**
 * マウント先の path を、ディレクトリとファイル名に分ける(最後の「/」で分ける)。
 *
 * @param path マウント先の path(例: /etc/app/app.conf)。
 * @returns ディレクトリ(例: /etc/app。ルートの直下なら空)と、ファイル名。
 */
function splitPath(path: string): { directory: string; name: string } {
  // 「/」が無ければ、全部ファイル名(入力の途中)。
  const index = path.lastIndexOf("/")
  if (index < 0) return { directory: "", name: path }
  return { directory: path.slice(0, index), name: path.slice(index + 1) }
}

/**
 * ディレクトリとファイル名を、マウント先の path にする。
 *
 * @param directory ディレクトリ(末尾の「/」は、取り除く)。
 * @param name ファイル名。
 * @returns path(例: /etc/app/app.conf)。
 */
function joinPath(directory: string, name: string): string {
  // 間に「/」を 1 つ入れる。
  return `${directory.replace(/\/+$/, "")}/${name}`
}

/** 「カスタム」を選んだときの、選択肢の値。 */
const CUSTOM_MODE = "custom"

type Props = {
  nav: Nav
  deployId: string
  projectId: string
  refreshInterval: number
  /** 反映中などで、編集できないか。 */
  locked: boolean
  busy: boolean
  /** 操作を実行して、結果を通知し、deploy を取り直す(deploy のパネルの run)。 */
  run: (action: () => Promise<unknown>, done: string) => Promise<void>
}

/**
 * マウントの 1 行を、差分に出す短い文字にする。
 *
 * @param row マウント。
 * @param names ConfigMap の ID から名前への対応。
 * @returns 「app-config/app.conf 版1 0644」の形。
 */
function describe(row: Row, names: Map<string, string>): string {
  // 名前が分からなければ、ID を出す。
  return `${names.get(row.configMapId) ?? row.configMapId}/${row.key} 版${row.revision || "最新"} ${row.mode || "0644"}`
}

/**
 * deploy の「ConfigMap」タブ。プロジェクトで共有する ConfigMap の 1 ファイルを、好きなパスにマウントする。
 *
 * 処理の流れ:
 *  1. 現在のマウントと、未反映のマウントを取る(ConfigMap の名前と、最新の版も付いてくる)。
 *  2. 行を編集して「保存」すると、未反映の変更になる。版は、保存のとき固定される。
 *  3. ConfigMap が編集されても、ここには届かない。「新しい版あり」と出るので、「最新にする」で版を上げて、保存・反映する。
 *  4. 「反映」で Pod が再作成され、マウントが変わる。
 *
 * @param props.deployId deploy の ID。
 * @param props.projectId プロジェクトの ID(ConfigMap の一覧を取る)。
 * @param props.locked 編集できない状態か。
 * @param props.run 操作の実行。
 */
export function ConfigMapMountsTab({ nav, deployId, projectId, refreshInterval, locked, busy, run }: Props) {
  const path = `/api/v1/deploy/${encodeURIComponent(deployId)}/configmap-mounts`
  const { data: state, mutate: mutateState } = useSWR<ConfigMapMountsState>(path, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const { data: list } = useSWR<ConfigMapListResponse>(`/api/v1/projects/${encodeURIComponent(projectId)}/configmaps?limit=100`, authenticatedFetcher, { refreshInterval })
  const configMaps = list?.configmaps ?? []
  const names = new Map(configMaps.map((item) => [item.configMapId, item.name]))
  const latestOf = new Map(configMaps.map((item) => [item.configMapId, item.currentRevision]))
  const pending = state?.pending_mounts ?? null
  const source = pending ?? state?.mounts ?? []
  const [rows, setRows] = useState<Row[]>([])
  const [problem, setProblem] = useState("")
  const [bulkConfigMapId, setBulkConfigMapId] = useState("")
  const [bulkDirectory, setBulkDirectory] = useState("/etc/app")
  const sourceKey = JSON.stringify(source)
  // サーバーの値が変わったら(保存した、反映が終わった、など)、下書きを、その内容に合わせる。
  useEffect(() => {
    setRows(source.map(({ configMapId, key, path: mountPath, mode, revision }) => ({ configMapId, key, path: mountPath, mode, revision })))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKey])

  if (!state) return <div className="card pad mut">読み込んでいます…</div>

  const outdated = rows.filter((row) => row.revision > 0 && (latestOf.get(row.configMapId) ?? 0) > row.revision)

  /**
   * 1 行を書き換える。
   *
   * @param index 行の位置。
   * @param patch 書き換える項目。
   */
  function updateRow(index: number, patch: Partial<Row>) {
    setRows((current) => current.map((row, at) => at === index ? { ...row, ...patch } : row))
  }

  /** 下書きを検査して、未反映の変更として保存する。 */
  function save() {
    // 個数、各項目、path の重複を確かめる(サーバーも確かめる)。
    if (rows.length > MAX_MOUNTS) { setProblem(`マウントは ${MAX_MOUNTS} 件までです。`); return }
    for (const row of rows) {
      if (!row.configMapId) { setProblem("ConfigMap を選んでください。"); return }
      const message = validateKey(row.key) || validateMountPath(row.path) || validateMode(row.mode)
      if (message) { setProblem(message); return }
    }
    if (new Set(rows.map((row) => row.path)).size !== rows.length) { setProblem("同じパスには、重ねてマウントできません。"); return }
    setProblem("")
    const body = { mounts: rows.map((row) => ({ configMapId: row.configMapId, key: row.key, path: row.path, ...(row.mode ? { mode: row.mode } : {}), ...(row.revision > 0 ? { revision: row.revision } : {}) })) }
    void run(async () => { await api.put(path, body); await Promise.all([mutateState(), revalidateProject(projectId)]) }, "ConfigMap のマウントを保存しました(未反映)")
  }

  /** ConfigMap の全ファイルを、1 つのディレクトリに、まとめて追加する(key ごとに、「ディレクトリ/key」の行を足す)。 */
  async function addDirectory() {
    // ディレクトリの書式を確かめてから、ConfigMap の中身(ファイル名)を取る。
    const directory = bulkDirectory.trim().replace(/\/+$/, "")
    const directoryProblem = validateMountPath(`${directory}/x`)
    if (!bulkConfigMapId) { setProblem("ConfigMap を選んでください。"); return }
    if (directoryProblem) { setProblem(`ディレクトリ「${bulkDirectory}」の形が正しくありません。`); return }
    try {
      const detail = await api.get<ConfigMapDetail>(`/api/v1/configmaps/${encodeURIComponent(bulkConfigMapId)}`)
      setProblem("")
      setRows((current) => {
        // すでにある path は、足さない。
        const used = new Set(current.map((row) => row.path))
        const added = detail.files.map((file) => ({ configMapId: bulkConfigMapId, key: file.key, path: `${directory}/${file.key}`, mode: "0644", revision: 0 })).filter((row) => !used.has(row.path))
        return [...current, ...added]
      })
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : "ConfigMap を取得できませんでした。")
    }
  }

  // 差分: 現在のマウントと、未反映のマウントを、path で比べる。
  const before = new Map((state.mounts ?? []).map((mount) => [mount.path, describe(mount, names)]))
  const after = new Map((pending ?? []).map((mount) => [mount.path, describe(mount, names)]))
  const diff: { cls: "d-add" | "d-chg" | "d-del"; text: string }[] = []
  for (const [mountPath, text] of after) {
    if (!before.has(mountPath)) diff.push({ cls: "d-add", text: `+ ${mountPath}  ←  ${text}` })
    else if (before.get(mountPath) !== text) diff.push({ cls: "d-chg", text: `~ ${mountPath}  ←  ${before.get(mountPath)} → ${text}` })
  }
  for (const [mountPath, text] of before) if (!after.has(mountPath)) diff.push({ cls: "d-del", text: `- ${mountPath}  (${text})` })

  return (
    <div className="col">
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <div className="row"><h2 className="h2 grow">ConfigMap のファイル</h2><button className="btn sm" type="button" onClick={() => setRows([...rows, { configMapId: "", key: "", path: "", mode: "0644", revision: 0 }])} disabled={locked}>+ 追加</button></div>
        {configMaps.length === 0 && list && <div className="mut">このプロジェクトに、ConfigMap がありません。<button className="lnk" type="button" onClick={() => nav.go("configmaps")}>ConfigMap を作る</button></div>}
        {rows.length === 0 && <div className="mut">マウントはありません。</div>}
        {outdated.length > 0 && (
          <div className="badge b-info" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px", gap: 10 }}>
            新しい版がある ConfigMap があります({outdated.length} 件)。
            <button className="btn sm" type="button" disabled={locked} onClick={() => setRows(rows.map((row) => row.revision > 0 && (latestOf.get(row.configMapId) ?? 0) > row.revision ? { ...row, revision: latestOf.get(row.configMapId) ?? row.revision } : row))}>すべて最新にする</button>
          </div>
        )}
        {rows.map((row, index) => <MountRow key={index} row={row} configMaps={configMaps} latest={latestOf.get(row.configMapId) ?? 0} locked={locked} onChange={(patch) => updateRow(index, patch)} onRemove={() => setRows(rows.filter((_, at) => at !== index))} />)}
        {problem && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{problem}</div>}
        <button className="btn primary" type="button" style={{ alignSelf: "flex-start" }} onClick={save} disabled={locked || busy}>保存</button>
        <div className="faint" style={{ fontSize: 12.5 }}>
          マウントしたあと ConfigMap を編集しても、このデプロイには届きません(版を固定しています)。「新しい版あり」と出たら、「最新にする」→「保存」→「反映」で、取り込みます。
        </div>
      </div>
      {/* ディレクトリへ、まとめて追加。 */}
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <h2 className="h2">全ファイルを、ディレクトリに追加</h2>
        <div className="row" style={{ flexWrap: "nowrap", gap: 8 }}>
          <select className="inp mono" style={{ flex: "1 1 40%", minWidth: 0 }} value={bulkConfigMapId} onChange={(event) => setBulkConfigMapId(event.target.value)} disabled={locked} aria-label="ConfigMap">
            <option value="">ConfigMap を選ぶ</option>
            {configMaps.map((item) => <option key={item.configMapId} value={item.configMapId}>{item.name}</option>)}
          </select>
          <span className="faint">→</span>
          <input className="inp mono" style={{ flex: "1 1 40%", minWidth: 0 }} value={bulkDirectory} onChange={(event) => setBulkDirectory(event.target.value)} disabled={locked} aria-label="ディレクトリ" placeholder="/etc/app" />
          <button className="btn sm" type="button" onClick={() => void addDirectory()} disabled={locked}>追加</button>
        </div>
        <div className="faint" style={{ fontSize: 12.5 }}>ファイルごとに「ディレクトリ/ファイル名」の行が足されます(保存するまで、反映されません)。</div>
      </div>
      <div className="col">
        {pending ? (
          <>
            <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}><h2 className="h2">変更内容の確認</h2><DiffBox lines={diff.length ? diff : [{ cls: "d-chg", text: "(ConfigMap のマウントを全て外す)" }]} /></div>
            <ApplyBar title="ConfigMap のマウントに未反映の変更があります" hint="反映すると Pod が再作成されます。" busy={locked || busy} onDiscard={() => void run(async () => { await api.post(`${path}/discard`); await Promise.all([mutateState(), revalidateProject(projectId)]) }, "変更を破棄しました")} onApply={() => void run(async () => { await api.post(`${path}/apply`); await Promise.all([mutateState(), revalidateProject(projectId)]) }, "反映を開始しました")} />
          </>
        ) : <div className="card pad mut">変更はありません。上で編集して保存すると、ここに変更内容が表示されます。</div>}
      </div>
    </div>
  )
}

/**
 * マウント 1 行(ConfigMap、ファイル名、マウント先、権限、版)。
 *
 * @param props.row 行。
 * @param props.configMaps プロジェクトの ConfigMap。
 * @param props.latest この行の ConfigMap の、今の最新の版(分からなければ 0)。
 * @param props.locked 編集できないか。
 * @param props.onChange 書き換えたとき。
 * @param props.onRemove 外すとき。
 */
function MountRow({ row, configMaps, latest, locked, onChange, onRemove }: { row: Row; configMaps: ConfigMapListResponse["configmaps"]; latest: number; locked: boolean; onChange: (patch: Partial<Row>) => void; onRemove: () => void }) {
  // 選んでいる ConfigMap の、ファイル名の候補(最新の版のもの。中身は大きいので、自動では取り直さない)。
  const { data: detail } = useSWR<ConfigMapDetail>(row.configMapId ? `/api/v1/configmaps/${encodeURIComponent(row.configMapId)}` : null, authenticatedFetcher, { revalidateOnFocus: false, revalidateIfStale: false })
  const keys = detail?.files.map((file) => file.key) ?? []
  const outdated = row.revision > 0 && latest > row.revision
  // プリセットに無い権限か、「カスタム」を選んだときは、4 桁を直接入力させる。
  const [forceCustom, setForceCustom] = useState(false)
  const { directory, name } = splitPath(row.path)
  const customMode = forceCustom || (row.mode !== "" && !MODE_PRESETS.some((preset) => preset.mode === row.mode))
  return (
    <div className="col" style={{ gap: 6 }}>
      <div className="row" style={{ flexWrap: "nowrap", gap: 8 }}>
        <select className="inp mono" style={{ flex: "1 1 30%", minWidth: 0 }} value={row.configMapId} onChange={(event) => onChange({ configMapId: event.target.value, key: "", revision: 0 })} disabled={locked} aria-label="ConfigMap">
          <option value="">ConfigMap を選ぶ</option>
          {configMaps.map((item) => <option key={item.configMapId} value={item.configMapId}>{item.name}</option>)}
          {row.configMapId && !configMaps.some((item) => item.configMapId === row.configMapId) && <option value={row.configMapId}>{row.configMapId}</option>}
        </select>
        <select className="inp mono" style={{ flex: "1 1 25%", minWidth: 0 }} value={row.key} onChange={(event) => {
          // ファイル名が空か、前のファイル名(key と同じ)のままなら、新しい key に合わせる。変えていれば、そのまま。
          const { directory, name } = splitPath(row.path)
          const keepName = name !== "" && name !== row.key
          onChange({ key: event.target.value, revision: 0, ...(keepName || !event.target.value ? {} : { path: joinPath(directory, event.target.value) }) })
        }} disabled={locked || !row.configMapId} aria-label="ファイル">
          <option value="">ファイルを選ぶ</option>
          {keys.map((key) => <option key={key} value={key}>{key}</option>)}
          {row.key && !keys.includes(row.key) && <option value={row.key}>{row.key}</option>}
        </select>
        <button className="btn ico ghost" type="button" onClick={onRemove} disabled={locked} aria-label="削除">×</button>
      </div>
      <div className="row" style={{ gap: 8, paddingLeft: 12 }}>
        <span className="faint">→</span>
        <input className="inp mono" style={{ flex: "1 1 30%", minWidth: 0 }} value={directory} onChange={(event) => onChange({ path: joinPath(event.target.value, name) })} disabled={locked} aria-label="マウント先のディレクトリ" placeholder="/etc/app" />
        <span className="faint">/</span>
        <input className="inp mono" style={{ flex: "1 1 25%", minWidth: 0 }} value={name} onChange={(event) => onChange({ path: joinPath(directory, event.target.value) })} disabled={locked} aria-label="マウント先のファイル名" placeholder={row.key || "app.conf"} />
        <select className="inp" style={{ width: 210, flex: "none" }} value={customMode ? CUSTOM_MODE : row.mode || "0644"} onChange={(event) => { if (event.target.value === CUSTOM_MODE) setForceCustom(true); else { setForceCustom(false); onChange({ mode: event.target.value }) } }} disabled={locked} aria-label="権限">
          {MODE_PRESETS.map((preset) => <option key={preset.mode} value={preset.mode}>{preset.label}</option>)}
          <option value={CUSTOM_MODE}>{customMode && row.mode ? `カスタム(${row.mode})` : "カスタム…"}</option>
        </select>
        {customMode && <input className="inp mono" style={{ width: 76, flex: "none" }} value={row.mode} onChange={(event) => onChange({ mode: event.target.value })} disabled={locked} aria-label="権限 (8 進数 4 桁)" placeholder="0644" maxLength={4} />}
      </div>
      {row.revision > 0 && (
        <div className="row" style={{ gap: 8, paddingLeft: 12, fontSize: 12.5 }}>
          <span className="mut mono">版 {row.revision}</span>
          {outdated && <><span className="badge b-info"><i />新しい版あり(最新は {latest})</span><button className="btn sm" type="button" disabled={locked} onClick={() => onChange({ revision: latest })}>最新にする</button></>}
        </div>
      )}
    </div>
  )
}

"use client"

import { useEffect, useRef, useState, type DragEvent, type FormEvent } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import {
  MAX_FILES, MAX_STORED_BYTES, type DraftFile, downloadDraftFile, draftFromFile, formatBytes, newUid, sameFiles, storedBytes,
  totalStoredBytes, utf8Length, validateDraftFiles, validateKey,
} from "@/lib/configmap-files"
import type { Nav, ProjectViewProps } from "@/lib/nav"
import type { ConfigMapDetail, ConfigMapListResponse, ConfigMapRevisionItem, ConfigMapUsage } from "@/lib/types"
import { KindIcon } from "@/components/kind-icon"
import { Badge, EmptyState, ErrorState, LoadingState, Modal, ToastNotice, formattedDate } from "@/components/ui-kit"

/** 名前の規則(volume と同じ。英数字とハイフンの 1〜64 文字)。 */
const NAME_PATTERN = /^[A-Za-z0-9-]{1,64}$/

/**
 * ConfigMap の画面(プロジェクトで共有する設定ファイルの組)。左に一覧、右に、選んだ ConfigMap の編集を出す。
 *
 * 処理の流れ:
 *  1. プロジェクトの ConfigMap の一覧を取る(中身は含まれない)。
 *  2. 1 つ選ぶと、その中身(最新の版)を取り、下書きとして編集できる。保存すると新しい版ができる。
 *  3. 使っている deploy を出す。使われている ConfigMap は、削除できない。
 *
 * @param props 共通の引数。
 */
export function ConfigMapsView({ project, refreshInterval, nav }: ProjectViewProps) {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}/configmaps`
  const { data, error, mutate } = useSWR<ConfigMapListResponse>(`${base}?limit=100`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const items = data?.configmaps ?? []
  // 選んでいる ConfigMap が無くなった(削除した)ら、選択を外す。
  useEffect(() => { if (data && selectedId && !items.some((item) => item.configMapId === selectedId)) setSelectedId(null) }, [data, items, selectedId])
  // 何も選んでいなければ、先頭を選ぶ。
  const activeId = selectedId ?? items[0]?.configMapId ?? null

  return (
    <div className="col" style={{ gap: 16 }}>
      <div className="row"><h1 className="h1 disp grow">ConfigMap</h1><button className="btn primary" type="button" onClick={() => setCreating(true)}>+ ConfigMap を作る</button></div>
      <div className="mut" style={{ marginTop: -8 }}>設定ファイルの組を、プロジェクトで共有します。デプロイは、ファイルを、好きなパスにマウントできます。</div>
      {error && !data && <ErrorState message="ConfigMap の一覧を取得できませんでした。" onRetry={() => void mutate()} />}
      {!data && !error && <LoadingState />}
      {data && items.length === 0 && <EmptyState title="ConfigMap はまだありません" description="「+ ConfigMap を作る」から、設定ファイルをアップロードするか、書いて追加できます。" />}
      {data && items.length > 0 && (
        <div style={{ display: "grid", gridTemplateColumns: "minmax(240px, 320px) minmax(0, 1fr)", gap: 16, alignItems: "start" }}>
          <div className="col" style={{ gap: 8 }} role="listbox" aria-label="ConfigMap の一覧">
            {items.map((item) => (
              <button key={item.configMapId} type="button" role="option" aria-selected={item.configMapId === activeId} className={`opt ${item.configMapId === activeId ? "on" : ""}`} style={{ height: "auto", padding: "10px 14px", flexDirection: "column", alignItems: "flex-start", gap: 4 }} onClick={() => setSelectedId(item.configMapId)}>
                <span className="row" style={{ gap: 8, flexWrap: "nowrap", width: "100%" }}><KindIcon kind="configmap" size={26} /><b style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.name}</b></span>
                <span className="mut" style={{ fontSize: 12.5 }}>版 {item.currentRevision} · {item.fileCount} ファイル · {formatBytes(item.totalBytes)} · {item.usedBy > 0 ? `${item.usedBy} 個のデプロイが使用中` : "未使用"}</span>
              </button>
            ))}
          </div>
          {activeId && <ConfigMapEditor key={activeId} project={project} nav={nav} refreshInterval={refreshInterval} configMapId={activeId} onChanged={() => mutate()} onDeleted={() => { setSelectedId(null); void mutate() }} />}
        </div>
      )}
      {creating && <CreateConfigMapModal base={base} onClose={() => setCreating(false)} onCreated={async (configMapId) => { setCreating(false); await mutate(); setSelectedId(configMapId) }} />}
    </div>
  )
}

/**
 * ConfigMap を作るモーダル(名前だけ。ファイルは、作ったあとに足す)。
 *
 * @param props.base 一覧の API のパス。
 * @param props.onClose 閉じるとき。
 * @param props.onCreated 作れたとき(ConfigMap の ID を渡す)。
 */
function CreateConfigMapModal({ base, onClose, onCreated }: { base: string; onClose: () => void; onCreated: (configMapId: string) => Promise<void> }) {
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  /**
   * 送信されたとき: 名前を検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // 手元で検査してから送る(サーバーも確かめる)。
    if (!NAME_PATTERN.test(name.trim())) return setError("名前は、英数字とハイフンだけの 1〜64 文字にしてください。")
    setBusy(true)
    setError("")
    try {
      const created = await api.post<ConfigMapDetail>(base, { name: name.trim(), files: [] })
      await onCreated(created.configMapId)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "ConfigMap を作成できませんでした")
      setBusy(false)
    }
  }

  return (
    <Modal title="ConfigMap を作る" description="名前を決めると、空の ConfigMap ができます。ファイルは、作ったあとに足します。" onClose={onClose} footer={<><button className="btn" type="button" onClick={onClose}>キャンセル</button><button className="btn primary" type="submit" form="configmap-form" disabled={busy}>作成</button></>}>
      <form id="configmap-form" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <label className="lbl">名前<input className="inp" value={name} onChange={(event) => setName(event.target.value)} placeholder="app-config" maxLength={64} autoFocus /></label>
        {error && <ErrorState message={error} />}
      </form>
    </Modal>
  )
}

type EditorProps = { project: ProjectViewProps["project"]; nav: Nav; refreshInterval: number; configMapId: string; onChanged: () => unknown; onDeleted: () => void }

/**
 * 1 つの ConfigMap の編集(ファイルの追加・編集・削除、版、使っている deploy、名前、削除)。
 *
 * ファイルは「下書き」として手元で編集し、「保存」で全件を置き換える。中身が同じなら、版は増えない。
 * 古い版は、読み取り専用で見られ、「この版で下書きを置き換える」で、戻す元にできる。
 *
 * @param props.configMapId 開いている ConfigMap の ID。
 * @param props.onChanged 一覧に出る値(版、大きさ)が変わったとき。
 * @param props.onDeleted 削除できたとき。
 */
function ConfigMapEditor({ nav, refreshInterval, configMapId, onChanged, onDeleted }: EditorProps) {
  const path = `/api/v1/configmaps/${encodeURIComponent(configMapId)}`
  // 中身は大きい(最大 1 MiB)ので、自動では取り直さない(保存したときと、画面を開いたときだけ)。
  const { data: latest, error, mutate: mutateLatest } = useSWR<ConfigMapDetail>(path, authenticatedFetcher, { revalidateOnFocus: false, revalidateIfStale: false })
  const { data: revisions, mutate: mutateRevisions } = useSWR<{ revisions: ConfigMapRevisionItem[] }>(`${path}/revisions`, authenticatedFetcher, { refreshInterval })
  const { data: usage, mutate: mutateUsage } = useSWR<{ usages: ConfigMapUsage[] }>(`${path}/usage`, authenticatedFetcher, { refreshInterval })
  const [viewRevision, setViewRevision] = useState<number | null>(null)
  const { data: old } = useSWR<ConfigMapDetail>(viewRevision !== null ? `${path}?revision=${viewRevision}` : null, authenticatedFetcher, { revalidateOnFocus: false })
  const [draft, setDraft] = useState<DraftFile[]>([])
  const [name, setName] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)
  const uploadRef = useRef<HTMLInputElement>(null)

  // 最新の版が変わったら(最初に取れたとき、保存したあと)、下書きを、その内容にする。
  const sourceKey = latest ? `${latest.configMapId}:${latest.revision}` : ""
  useEffect(() => {
    if (latest) setDraft(latest.files.map((file) => ({ ...file, uid: newUid() })))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKey])

  /**
   * 通知を出す。
   *
   * @param message 本文。
   * @param isError 失敗なら true。
   */
  function notify(message: string, isError = false) { setToast({ message, error: isError }) }

  /**
   * 操作を実行し、失敗は通知する。
   *
   * @param action 実行する処理。
   */
  async function run(action: () => Promise<void>) {
    // 実行中の印を付け、失敗なら通知する。
    setBusy(true)
    try { await action() } catch (caught) { notify(caught instanceof Error ? caught.message : "操作に失敗しました", true) } finally { setBusy(false) }
  }

  /**
   * 選んだファイルを、下書きに足す(同じ名前があれば置き換える)。
   *
   * @param files ブラウザのファイル。
   */
  async function addFiles(files: FileList | File[]) {
    // 1 つずつ読み込み、text か base64 かを決めて、足す。
    const added: DraftFile[] = []
    for (const file of Array.from(files)) added.push(await draftFromFile(file))
    if (added.length === 0) return
    setDraft((current) => {
      const addedKeys = new Set(added.map((file) => file.key))
      return [...current.filter((file) => !addedKeys.has(file.key)), ...added].sort((left, right) => left.key.localeCompare(right.key))
    })
    notify(`${added.length} 個のファイルを足しました(未保存)`)
  }

  /**
   * ドロップされたとき: ファイルを足す。
   *
   * @param event ドロップのイベント。
   */
  function onDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    setDragging(false)
    void addFiles(event.dataTransfer.files)
  }

  /**
   * 下書きの 1 つのファイルを、書き換える(text の内容を変えたら、大きさも合わせる)。
   *
   * @param uid 行の識別子。
   * @param patch 書き換える項目。
   */
  function updateFile(uid: string, patch: Partial<DraftFile>) {
    setDraft((current) => current.map((file) => {
      if (file.uid !== uid) return file
      const next = { ...file, ...patch }
      if (patch.content !== undefined && next.encoding === "text") next.size = utf8Length(next.content)
      return next
    }))
  }

  if (error && !latest) return <ErrorState message="ConfigMap を取得できませんでした。" onRetry={() => void mutateLatest()} />
  if (!latest) return <LoadingState />

  const problem = validateDraftFiles(draft)
  const dirty = !sameFiles(draft, latest.files)
  const total = totalStoredBytes(draft)
  const ratio = Math.min(1, total / MAX_STORED_BYTES)
  const usages = usage?.usages ?? []
  const usedDeployIds = Array.from(new Set(usages.map((entry) => entry.deployId)))
  const viewingOld = viewRevision !== null && viewRevision !== latest.currentRevision

  /** 保存する: 全件を置き換える。同じ中身なら、版は増えない。 */
  async function save() {
    await run(async () => {
      const saved = await api.put<ConfigMapDetail>(path, { files: draft.map(({ key, encoding, content }) => ({ key, encoding, content })) })
      await Promise.all([mutateLatest(saved, { revalidate: false }), mutateRevisions(), onChanged()])
      notify(saved.currentRevision === latest!.currentRevision ? "変更はありません(版は増えません)" : `版 ${saved.currentRevision} を作りました。使っているデプロイには、まだ届きません(デプロイの「ConfigMap」で、最新にして反映します)`)
    })
  }

  /** 名前を変える。 */
  async function rename() {
    await run(async () => {
      await api.patch(path, { name: (name ?? latest!.name).trim() })
      setName(null)
      await Promise.all([mutateLatest(), onChanged()])
      notify("名前を変更しました")
    })
  }

  /** 削除する。使われている ConfigMap は、サーバーが 409 で断る(メッセージに、使っているデプロイが入る)。 */
  async function remove() {
    await run(async () => {
      await api.delete(path)
      setConfirming(false)
      onDeleted()
    })
    setConfirming(false)
  }

  return (
    <div className="col" style={{ gap: 16, minWidth: 0 }}>
      {/* 見出し: 名前の変更、版の切り替え、削除。 */}
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div className="row" style={{ gap: 8 }}>
          <KindIcon kind="configmap" size={34} />
          <input className="inp" style={{ flex: "1 1 200px", maxWidth: 320 }} value={name ?? latest.name} onChange={(event) => setName(event.target.value)} aria-label="名前" />
          <button className="btn sm" type="button" disabled={busy || name === null || name.trim() === latest.name || !NAME_PATTERN.test(name.trim())} onClick={() => void rename()}>名前変更</button>
          <span className="grow" />
          <label className="row" style={{ gap: 6 }}>
            <span className="mut">版</span>
            <select className="inp mono" style={{ width: "auto", minHeight: 36 }} value={viewRevision ?? "latest"} onChange={(event) => setViewRevision(event.target.value === "latest" ? null : Number(event.target.value))} aria-label="表示する版">
              <option value="latest">最新(版 {latest.currentRevision})</option>
              {(revisions?.revisions ?? []).filter((item) => item.revision !== latest.currentRevision).map((item) => <option key={item.revision} value={item.revision}>版 {item.revision}({formattedDate(item.createdAt)})</option>)}
            </select>
          </label>
          <button className="btn sm danger" type="button" disabled={busy} onClick={() => setConfirming(true)}>削除</button>
        </div>
        <div className="faint" style={{ fontSize: 12.5 }}>編集して保存すると、新しい版ができます。使っているデプロイは、版を固定しているので、自動では変わりません。</div>
      </div>

      {viewingOld ? (
        <OldRevisionView revision={viewRevision!} detail={old} onUse={() => {
          if (!old) return
          setDraft(old.files.map((file) => ({ ...file, uid: newUid() })))
          setViewRevision(null)
          notify(`版 ${old.revision} の内容を、下書きにしました(未保存)`)
        }} />
      ) : (
        <>
          {/* ファイルの追加。 */}
          <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <div className="row"><h2 className="h2 grow">ファイル({draft.length})</h2>
              <button className="btn sm" type="button" onClick={() => uploadRef.current?.click()} disabled={busy || draft.length >= MAX_FILES}>アップロード</button>
              <button className="btn sm" type="button" onClick={() => setDraft((current) => [...current, { uid: newUid(), key: "", encoding: "text", content: "", size: 0 }])} disabled={busy || draft.length >= MAX_FILES}>+ 書いて追加</button>
              <input ref={uploadRef} type="file" multiple hidden onChange={(event) => { if (event.target.files) void addFiles(event.target.files); event.target.value = "" }} />
            </div>
            <div
              onDragOver={(event) => { event.preventDefault(); setDragging(true) }} onDragLeave={() => setDragging(false)} onDrop={onDrop}
              style={{ border: `1.5px dashed ${dragging ? "var(--accent)" : "var(--line2)"}`, borderRadius: 8, padding: "12px 14px", textAlign: "center", background: dragging ? "color-mix(in srgb, var(--accent) 8%, #fff)" : "transparent" }}
            >
              <span className="mut">ここにファイルをドロップ(複数可)。UTF-8 のテキストはそのまま、それ以外はバイナリとして保存します。</span>
            </div>
            {/* 大きさの目盛り。 */}
            <div>
              <div className="row" style={{ justifyContent: "space-between", fontSize: 12.5 }}><span className="mut">保存する大きさ</span><span className="mono" style={{ color: total > MAX_STORED_BYTES ? "var(--bad)" : undefined }}>{formatBytes(total)} / {formatBytes(MAX_STORED_BYTES)}</span></div>
              <div style={{ height: 6, borderRadius: 3, background: "var(--line)", overflow: "hidden", marginTop: 4 }}><div style={{ width: `${ratio * 100}%`, height: "100%", background: total > MAX_STORED_BYTES ? "var(--bad)" : "var(--accent)" }} /></div>
              <div className="faint" style={{ fontSize: 12, marginTop: 4 }}>バイナリは、base64 にした後の大きさ(元の約 1.33 倍)で数えます。</div>
            </div>
            {draft.length === 0 && <div className="mut">ファイルはありません。アップロードするか、「+ 書いて追加」で書いてください。</div>}
            {draft.map((file) => <DraftFileRow key={file.uid} file={file} busy={busy} duplicate={draft.filter((other) => other.key === file.key).length > 1} onChange={(patch) => updateFile(file.uid, patch)} onRemove={() => setDraft((current) => current.filter((other) => other.uid !== file.uid))} onReplace={async (picked) => { const replaced = await draftFromFile(picked); updateFile(file.uid, { encoding: replaced.encoding, content: replaced.content, size: replaced.size }) }} />)}
            {problem && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal" }}>{problem}</div>}
            <div className="row" style={{ gap: 8 }}>
              <button className="btn primary" type="button" disabled={busy || !dirty || problem !== ""} onClick={() => void save()}>保存</button>
              <button className="btn" type="button" disabled={busy || !dirty} onClick={() => setDraft(latest.files.map((file) => ({ ...file, uid: newUid() })))}>変更を破棄</button>
              {dirty && <span className="badge b-warn"><i />未保存の変更があります</span>}
            </div>
            <div className="faint" style={{ fontSize: 12.5 }}>内容は、暗号化せずに保存します。パスワードや秘密鍵は、置かないでください。</div>
          </div>
        </>
      )}

      {/* 使っているデプロイ。 */}
      <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <h2 className="h2">使っているデプロイ({usedDeployIds.length})</h2>
        {usedDeployIds.length === 0 && <div className="mut">どのデプロイもマウントしていません。デプロイの「ConfigMap」タブで、マウントできます。</div>}
        {usedDeployIds.map((deployId) => (
          <div key={deployId} className="col" style={{ gap: 4 }}>
            <button className="opt" type="button" onClick={() => nav.openDeploy(deployId, "configmap")}><span className="mono">{deployId}</span></button>
            {usages.filter((entry) => entry.deployId === deployId).map((entry) => (
              <div key={`${entry.key}:${entry.path}:${entry.pending}`} className="mut mono" style={{ fontSize: 12.5, paddingLeft: 12 }}>
                版 {entry.revision} · {entry.key} → {entry.path} {entry.pending && <span className="badge b-warn"><i />未反映</span>}
                {!entry.pending && entry.revision < latest.currentRevision && <span className="badge b-info" style={{ marginLeft: 6 }}><i />新しい版あり</span>}
              </div>
            ))}
          </div>
        ))}
      </div>

      {confirming && (
        <Modal title="ConfigMap を削除しますか?" description={latest.name} onClose={() => setConfirming(false)} footer={<><button className="btn" type="button" onClick={() => setConfirming(false)}>キャンセル</button><button className="btn warn" type="button" disabled={busy || usedDeployIds.length > 0} onClick={() => void remove()}>削除</button></>}>
          {usedDeployIds.length > 0
            ? <div className="col" style={{ gap: 6 }}><div style={{ color: "var(--bad)" }}>次のデプロイが使っているため、削除できません。先に、デプロイのマウントを外してください。</div>{usedDeployIds.map((deployId) => <button key={deployId} className="lnk mono" type="button" onClick={() => { setConfirming(false); nav.openDeploy(deployId, "configmap") }}>{deployId}</button>)}</div>
            : <div className="mut">全ての版が削除されます。元に戻せません。</div>}
        </Modal>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => { setToast(null); void mutateUsage() }} />}
    </div>
  )
}

/**
 * 下書きのファイル 1 つの行。text は、その場で編集でき、バイナリは、差し替えとダウンロードだけ。
 *
 * @param props.file 下書きのファイル。
 * @param props.busy 操作中か。
 * @param props.duplicate 同じ名前が、他にもあるか。
 * @param props.onChange 書き換えたとき。
 * @param props.onRemove 外すとき。
 * @param props.onReplace 差し替えのファイルを選んだとき(バイナリ用)。
 */
function DraftFileRow({ file, busy, duplicate, onChange, onRemove, onReplace }: { file: DraftFile; busy: boolean; duplicate: boolean; onChange: (patch: Partial<DraftFile>) => void; onRemove: () => void; onReplace: (picked: File) => Promise<void> }) {
  const replaceRef = useRef<HTMLInputElement>(null)
  const keyProblem = file.key === "" ? "" : validateKey(file.key)
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8, background: "var(--panel2)" }}>
      <div className="row" style={{ gap: 8, flexWrap: "nowrap" }}>
        <input className={`inp mono ${keyProblem || duplicate ? "chg" : ""}`} style={{ flex: 1, minWidth: 0 }} value={file.key} onChange={(event) => onChange({ key: event.target.value })} placeholder="app.conf" aria-label="ファイル名" maxLength={253} />
        <span className={`badge ${file.encoding === "text" ? "b-dim" : "b-info"}`}><i />{file.encoding === "text" ? "テキスト" : "バイナリ"}</span>
        <span className="mut mono" style={{ whiteSpace: "nowrap" }}>{formatBytes(file.size)}</span>
        <span className="faint mono" style={{ fontSize: 12, whiteSpace: "nowrap" }} title="保存する大きさ">({formatBytes(storedBytes(file))})</span>
        <button className="btn sm" type="button" onClick={() => downloadDraftFile(file)} disabled={file.key === ""} aria-label="ダウンロード">ダウンロード</button>
        <button className="btn ico ghost" type="button" onClick={onRemove} disabled={busy} aria-label="ファイルを外す">×</button>
      </div>
      {file.encoding === "text" ? (
        <textarea className="inp mono" style={{ minHeight: 140, padding: 10, resize: "vertical", whiteSpace: "pre", overflow: "auto", lineHeight: 1.45 }} value={file.content} onChange={(event) => onChange({ content: event.target.value })} spellCheck={false} aria-label={`${file.key || "新しいファイル"}の内容`} />
      ) : (
        <div className="row" style={{ gap: 8 }}>
          <span className="mut">バイナリ({formatBytes(file.size)})は、画面では編集できません。</span>
          <button className="btn sm" type="button" onClick={() => replaceRef.current?.click()} disabled={busy}>差し替え</button>
          <input ref={replaceRef} type="file" hidden onChange={(event) => { const picked = event.target.files?.[0]; event.target.value = ""; if (picked) void onReplace(picked) }} />
        </div>
      )}
      {(keyProblem || duplicate) && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{duplicate ? `ファイル名「${file.key}」が重複しています。` : keyProblem}</div>}
    </div>
  )
}

/**
 * 古い版の、読み取り専用の表示。「この版で、下書きを置き換える」で、戻す元にできる。
 *
 * @param props.revision 見ている版。
 * @param props.detail その版の中身(取れるまで undefined)。
 * @param props.onUse この版の内容で、下書きを置き換えるとき。
 */
function OldRevisionView({ revision, detail, onUse }: { revision: number; detail: ConfigMapDetail | undefined; onUse: () => void }) {
  if (!detail) return <div className="card pad"><LoadingState label={`版 ${revision} を読み込んでいます`} /></div>
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div className="row"><h2 className="h2 grow">版 {detail.revision}(読み取り専用)</h2><Badge status="success" /><button className="btn sm primary" type="button" onClick={onUse}>この版の内容で、下書きを置き換える</button></div>
      {detail.files.length === 0 && <div className="mut">この版には、ファイルがありません。</div>}
      {detail.files.map((file) => (
        <div key={file.key} className="col" style={{ gap: 4 }}>
          <div className="row" style={{ gap: 8 }}><b className="mono">{file.key}</b><span className={`badge ${file.encoding === "text" ? "b-dim" : "b-info"}`}><i />{file.encoding === "text" ? "テキスト" : "バイナリ"}</span><span className="mut mono">{formatBytes(file.size)}</span></div>
          {file.encoding === "text" && <pre className="mono" style={{ margin: 0, padding: 10, background: "var(--panel2)", borderRadius: 6, maxHeight: 220, overflow: "auto", whiteSpace: "pre", fontSize: 12.5 }}>{file.content}</pre>}
        </div>
      ))}
    </div>
  )
}

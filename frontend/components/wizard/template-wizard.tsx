"use client"

import { useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { Activity, BarChart3, Box, Code2, Database, GitBranch, Globe, HardDrive, Mail, PenTool, Server, ShieldCheck, StickyNote, Workflow, type LucideIcon } from "lucide-react"
import { ApiError, api, authenticatedFetcher } from "@/lib/api"
import { revalidateProject } from "@/lib/revalidate"
import { CATEGORY_LABELS, buildValues, initialFields, renderNotes, stepLabel, summaryText, validateFields, type FieldState } from "@/lib/template-form"
import type { Nav } from "@/lib/nav"
import type { IngressDetail, Project, QuotaResponse, Template, TemplateApplyProgress, TemplateListResponse, TemplateVariable } from "@/lib/types"

/** テンプレートの icon(API の値)から、絵を決める。知らない名前は、箱の絵。 */
const ICONS: Record<string, LucideIcon> = {
  database: Database, globe: Globe, code: Code2, activity: Activity, shield: ShieldCheck, note: StickyNote, workflow: Workflow,
  git: GitBranch, storage: HardDrive, mail: Mail, draw: PenTool, chart: BarChart3, server: Server,
}

/** ウィザードの段階。 */
type Phase = "pick" | "form" | "confirm" | "running" | "done" | "failed"

/** 上に出す、段階の名前と、各段階が、どの印に当たるか。 */
const STEP_LABELS = ["テンプレート", "入力", "確認", "作成"]
const STEP_OF: Record<Phase, number> = { pick: 0, form: 1, confirm: 2, running: 3, done: 3, failed: 3 }

/** 進み具合を取る間隔(ミリ秒)と、取得の失敗を許す、連続の回数。 */
const POLL_MS = 2000
const MAX_POLL_FAILURES = 5

/**
 * 指定のミリ秒、待つ。
 *
 * @param milliseconds 待つ時間。
 * @returns 待ち終わると解決する Promise。
 */
function sleep(milliseconds: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds))
}

/**
 * テンプレートの絵。
 *
 * @param props.icon API の icon の名前。
 */
function TemplateIcon({ icon }: { icon: string }) {
  const Icon = ICONS[icon] ?? Box
  return <span className="kind-icon k-deploy" style={{ width: 36, height: 36 }} aria-hidden="true"><Icon size={20} /></span>
}

/**
 * テンプレートから作るウィザード。テンプレート → 入力 → 確認 → 作成の順に進む。
 * 作成は、サーバーが非同期で行う。画面は、進み具合(部品ごとの、待機中 → 作成中 → 完了)を、2 秒ごとに取って出す。
 * 閉じても、作成は続く。途中で失敗したら、そこで止まり、作れたものは、そのまま残る。
 *
 * @param props.project 作る先のプロジェクト。
 * @param props.nav 画面の切り替え(完了後に、構成図を開く)。
 * @param props.onClose 閉じるとき。
 */
export function TemplateWizard({ project, nav, onClose }: { project: Project; nav: Nav; onClose: () => void }) {
  const [phase, setPhase] = useState<Phase>("pick")
  const [category, setCategory] = useState("all")
  const [selected, setSelected] = useState<Template | null>(null)
  const [fields, setFields] = useState<Record<string, FieldState>>({})
  const [problems, setProblems] = useState<Record<string, string>>({})
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const [progress, setProgress] = useState<TemplateApplyProgress | null>(null)
  const [values, setValues] = useState<Record<string, string>>({})
  const [host, setHost] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [copied, setCopied] = useState("")
  const alive = useRef(true)
  const dialogRef = useRef<HTMLDivElement>(null)
  const projectPath = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  const { data: templateData, error: loadError, mutate: reload } = useSWR<TemplateListResponse>("/api/v1/templates", authenticatedFetcher, { revalidateOnFocus: false })
  const { data: quota } = useSWR<QuotaResponse>("/api/v1/quota", authenticatedFetcher, { revalidateOnFocus: false })
  const templates = templateData?.templates ?? []
  const categories = Array.from(new Set(templates.map((template) => template.category).filter(Boolean)))
  const visible = templates.filter((template) => category === "all" || template.category === category)

  // 開いたらダイアログにフォーカスを移す。閉じたら、進み具合の取得を止める(作成そのものは、サーバーで続く)。
  useEffect(() => {
    alive.current = true
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    window.requestAnimationFrame(() => dialogRef.current?.focus())
    return () => {
      alive.current = false
      previous?.focus()
    }
  }, [])

  // Esc で閉じる(受け付けの通信中は、閉じない)。
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && !busy) onClose()
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [busy, onClose])

  /**
   * テンプレートを選んで、入力の段階に進む。
   *
   * @param template 選んだテンプレート。
   */
  function choose(template: Template) {
    setSelected(template)
    setFields(initialFields(template))
    setProblems({})
    setRevealed({})
    setError("")
    setPhase("form")
  }

  /**
   * 入力欄 1 つを書き換える。
   *
   * @param name 変数名。
   * @param patch 書き換える項目。
   */
  function updateField(name: string, patch: Partial<FieldState>) {
    setFields((current) => ({ ...current, [name]: { ...current[name], ...patch } }))
    setProblems((current) => {
      const { [name]: _removed, ...rest } = current
      return rest
    })
  }

  /** 「次へ」(入力 → 確認): 全ての入力を検査して、問題がなければ進む。 */
  function toConfirm() {
    if (!selected) return
    const found = validateFields(selected, fields)
    setProblems(found)
    setError(Object.keys(found).length > 0 ? "入力に問題があります。赤く示した欄を直してください。" : "")
    if (Object.keys(found).length === 0) setPhase("confirm")
  }

  /**
   * 作成が終わったとき、公開 URL(ingress のホスト名)を取る。取れなくても、作成の結果は変わらない。
   *
   * @param result 完了した実行。
   */
  async function lookupHost(result: TemplateApplyProgress) {
    const ingress = result.steps.find((step) => step.kind === "ingress" && step.resourceId)
    if (!ingress) return
    try {
      const detail = await api.get<IngressDetail>(`${projectPath}/ingresses/${encodeURIComponent(ingress.resourceId)}`)
      if (alive.current) setHost(detail.host)
    } catch {
      // 取れなくても、構成図で見られる。
    }
  }

  /** 「作成する」: 受け付けを頼み、進み具合を、終わるまで、取り続ける。 */
  async function start() {
    if (!selected || busy) return
    setBusy(true)
    setError("")
    let applyId = ""
    try {
      const accepted = await api.post<{ applyId: string; values: Record<string, string> }>(`${projectPath}/templates/${encodeURIComponent(selected.id)}/apply`, { values: buildValues(selected, fields) })
      applyId = accepted.applyId
      setValues(accepted.values)
      setPhase("running")
    } catch (caught) {
      // 受け付けられなかった(入力の不正、クォータなど)。何も作られていないので、確認の画面に戻る。
      setError(caught instanceof ApiError || caught instanceof Error ? caught.message : "作成を始められませんでした。")
      setBusy(false)
      return
    }
    setBusy(false)
    // 進み具合を、終わるまで取る。取得の失敗は、連続で何度か許す(作成は、サーバーで続いている)。
    let failures = 0
    for (;;) {
      try {
        const current = await api.get<TemplateApplyProgress>(`${projectPath}/template-applies/${encodeURIComponent(applyId)}`)
        failures = 0
        if (!alive.current) return
        setProgress(current)
        void revalidateProject(project.projectId)
        if (current.status === "succeeded") {
          setPhase("done")
          void lookupHost(current)
          return
        }
        if (current.status === "failed") {
          setError(current.error ?? "作成に失敗しました。")
          setPhase("failed")
          return
        }
      } catch (caught) {
        failures += 1
        if (failures >= MAX_POLL_FAILURES) {
          if (alive.current) {
            setError((caught instanceof Error ? caught.message : "進み具合を取得できませんでした。") + "(作成は、続いている可能性があります。構成図を確かめてください)")
            setPhase("failed")
          }
          return
        }
      }
      await sleep(POLL_MS)
      if (!alive.current) return
    }
  }

  /**
   * 値をクリップボードにコピーする。
   *
   * @param name 変数名(コピーしたことを、この欄に示す)。
   * @param value コピーする値。
   */
  async function copy(name: string, value: string) {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(name)
      window.setTimeout(() => setCopied(""), 1500)
    } catch {
      setError("コピーできませんでした。手で選んで、コピーしてください。")
    }
  }

  /** 完了後に「構成図を見る」: 閉じて、構成図を開く。 */
  function openCanvas() {
    onClose()
    nav.go("canvas")
  }

  const deploysLeft = quota ? quota.deploys.limit - quota.deploys.used : undefined
  const volumesLeft = quota ? quota.volumes.limit - quota.volumes.used : undefined
  const quotaWarning = selected && ((deploysLeft !== undefined && selected.summary.deploys > deploysLeft) || (volumesLeft !== undefined && selected.summary.volumes > volumesLeft))
  const activeStep = STEP_OF[phase]

  return (
    <div className="modalbg" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}>
      <div ref={dialogRef} className="modal" role="dialog" aria-modal="true" aria-labelledby="template-wizard-title" tabIndex={-1} style={{ width: "min(820px,100%)", minHeight: 520 }}>
        <div className="row" style={{ gap: 12, flexWrap: "nowrap" }}>
          {selected && phase !== "pick" ? <TemplateIcon icon={selected.icon} /> : <span className="kind-icon k-deploy" style={{ width: 36, height: 36 }} aria-hidden="true"><Box size={20} /></span>}
          <div className="grow"><h2 id="template-wizard-title" className="h2" style={{ fontSize: 20 }}>{selected && phase !== "pick" ? selected.name : "テンプレートから作る"}</h2><div className="mut" style={{ fontSize: 13 }}>{project.name} に、よく使う構成を、まとめて作ります。</div></div>
          <button className="btn ico ghost" type="button" onClick={onClose} disabled={busy} aria-label="閉じる"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
        </div>

        {/* 段階の印。 */}
        <ol className="row" style={{ gap: 8, listStyle: "none", padding: 0, margin: 0 }} aria-label="進み具合">
          {STEP_LABELS.map((label, index) => {
            const active = activeStep === index
            const finished = activeStep > index || phase === "done"
            return (
              <li key={label} className="step" style={{ alignItems: "center", flex: "1 1 120px" }} aria-current={active ? "step" : undefined}>
                <span className="stepn" style={finished ? { background: "var(--accent)", color: "var(--ink)", borderColor: "var(--accent)" } : active ? { borderColor: "var(--accent)", color: "var(--accent)" } : undefined}>{finished ? "✓" : index + 1}</span>
                <span style={{ fontWeight: active ? 500 : 400, color: active ? "var(--text)" : "var(--mut)" }}>{label}</span>
              </li>
            )
          })}
        </ol>

        <div style={{ display: "flex", flexDirection: "column", gap: 14, flex: 1 }}>
          {/* 1. テンプレートを選ぶ */}
          {phase === "pick" && (
            <>
              {!templateData && !loadError && <div className="row mut"><span className="loader" />テンプレートを読み込んでいます</div>}
              {loadError && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px", gap: 10 }}>テンプレートを取得できませんでした。<button className="btn sm" type="button" onClick={() => void reload()}>再試行</button></div>}
              {templateData && (
                <>
                  <div className="row" style={{ gap: 8 }} role="group" aria-label="カテゴリ">
                    <button type="button" className={`chip ${category === "all" ? "on" : ""}`} onClick={() => setCategory("all")}>すべて</button>
                    {categories.map((item) => <button key={item} type="button" className={`chip ${category === item ? "on" : ""}`} onClick={() => setCategory(item)}>{CATEGORY_LABELS[item] ?? item}</button>)}
                  </div>
                  <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(230px, 1fr))", gap: 10 }}>
                    {visible.map((template) => (
                      <button key={template.id} type="button" className="opt" onClick={() => choose(template)} style={{ flexDirection: "column", alignItems: "flex-start", gap: 6, padding: 14, minHeight: 130, height: "auto" }}>
                        <span className="row" style={{ gap: 10, flexWrap: "nowrap" }}><TemplateIcon icon={template.icon} /><b>{template.name}</b></span>
                        <span className="mut" style={{ fontSize: 13 }}>{template.description}</span>
                        <span className="faint" style={{ fontSize: 12 }}>{summaryText(template)}</span>
                      </button>
                    ))}
                  </div>
                  {visible.length === 0 && <div className="mut">このカテゴリに、テンプレートはありません。</div>}
                </>
              )}
            </>
          )}

          {/* 2. 入力 */}
          {phase === "form" && selected && (
            <>
              <div className="mut">{selected.description}</div>
              {selected.variables.length === 0 && <div className="mut">入力する項目はありません。</div>}
              {selected.variables.map((variable) => (
                <FieldInput key={variable.name} variable={variable} field={fields[variable.name]} problem={problems[variable.name]} revealed={revealed[variable.name] === true}
                  onChange={(patch) => updateField(variable.name, patch)} onToggleReveal={() => setRevealed((current) => ({ ...current, [variable.name]: !current[variable.name] }))} />
              ))}
            </>
          )}

          {/* 3. 確認 */}
          {phase === "confirm" && selected && (
            <>
              <div className="mut">この内容で作成します。</div>
              <dl className="kv card pad" style={{ margin: 0 }}>
                <div><dt>作るもの</dt><dd>{summaryText(selected)}</dd></div>
                {selected.variables.map((variable) => {
                  const field = fields[variable.name]
                  const shown = variable.type === "password" ? (field.auto ? "自動で作る" : "●●●●●●●●") : variable.type === "choice" ? (variable.options.find((option) => option.value === field.value)?.label ?? field.value) : field.value === "" ? `${variable.default ?? ""}(既定)` : field.value
                  return <div key={variable.name}><dt>{variable.label}</dt><dd className="mono">{shown}</dd></div>
                })}
              </dl>
              {quotaWarning && <div className="badge b-warn" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>デプロイかボリュームの数が、上限を超える可能性があります(クォータを確かめてください)。超える場合は、何も作らずに、お断りします。</div>}
              <div className="faint" style={{ fontSize: 13 }}>作成には、数分かかることがあります。閉じても、作成は続きます。</div>
            </>
          )}

          {/* 4. 作成の進み具合 */}
          {(phase === "running" || phase === "done" || phase === "failed") && (
            <>
              <div className="mut">{phase === "done" ? "作成が終わりました。" : phase === "failed" ? "途中で失敗しました。作られたものは、そのまま残っています。" : "作成しています。ボリュームの用意などで、数分かかることがあります。閉じても、作成は続きます。"}</div>
              <ul className="card" style={{ listStyle: "none", margin: 0, padding: 0 }} aria-live="polite">
                {(progress?.steps ?? []).map((step) => (
                  <li key={`${step.kind}:${step.name}`} className="tr" style={{ ["--cols" as string]: "1fr auto" }}>
                    <span>{stepLabel(step)}{step.error && <span style={{ display: "block", color: "var(--bad)", fontSize: 12.5 }}>{step.error}</span>}</span>
                    <span className={`badge ${step.status === "done" ? "b-ok" : step.status === "failed" ? "b-bad" : step.status === "creating" ? "b-info live" : "b-dim"}`}><i />{step.status === "done" ? "完了" : step.status === "failed" ? "失敗" : step.status === "creating" ? "作成中" : "待機中"}</span>
                  </li>
                ))}
                {!progress && <li className="tr mut" style={{ ["--cols" as string]: "1fr" }}>受け付けました。準備しています…</li>}
              </ul>
              {phase === "done" && host && <div className="card pad row"><span className="mut">公開 URL</span><a className="lnk mono" href={`https://${host}`} target="_blank" rel="noreferrer">https://{host}</a><span className="faint" style={{ fontSize: 12.5 }}>反映と DNS が行き渡るまで、数分かかることがあります。</span></div>}
              {phase === "done" && selected && selected.notes && (
                <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                  <h2 className="h2">最初にすること</h2>
                  <div style={{ whiteSpace: "pre-wrap", lineHeight: 1.8, fontSize: 14 }}>{renderNotes(selected.notes, selected, values)}</div>
                </div>
              )}
              {phase === "done" && selected && <ResultValues template={selected} values={values} revealed={revealed} copied={copied} onToggleReveal={(name) => setRevealed((current) => ({ ...current, [name]: !current[name] }))} onCopy={(name, value) => void copy(name, value)} />}
            </>
          )}

          {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>{error}</div>}
        </div>

        {/* 下のボタン */}
        <div className="row" style={{ justifyContent: "flex-end" }}>
          {phase === "pick" && <button className="btn" type="button" onClick={onClose}>キャンセル</button>}
          {phase === "form" && <><button className="btn" type="button" onClick={() => { setError(""); setPhase("pick") }}>戻る</button><button className="btn primary" type="button" onClick={toConfirm}>次へ</button></>}
          {phase === "confirm" && <><button className="btn" type="button" onClick={() => { setError(""); setPhase("form") }} disabled={busy}>戻る</button><button className="btn primary" type="button" onClick={() => void start()} disabled={busy}>{busy ? "受け付けています…" : "作成する"}</button></>}
          {phase === "running" && <button className="btn" type="button" onClick={onClose}>閉じる(作成は続きます)</button>}
          {(phase === "failed") && <><button className="btn" type="button" onClick={onClose}>閉じる</button><button className="btn primary" type="button" onClick={openCanvas}>構成図を見る</button></>}
          {phase === "done" && <><button className="btn" type="button" onClick={onClose}>閉じる</button><button className="btn primary" type="button" onClick={openCanvas}>構成図を見る</button></>}
        </div>
      </div>
    </div>
  )
}

/**
 * 変数 1 つの入力欄。型ごとに、入力の形が変わる。password は、「自動で作る」と、手で入れる、を選べる。
 *
 * @param props.variable 変数の定義。
 * @param props.field 入力の状態。
 * @param props.problem 検査の結果の説明(無ければ undefined)。
 * @param props.revealed password を、表示しているか。
 * @param props.onChange 書き換えたとき。
 * @param props.onToggleReveal password の表示を切り替えるとき。
 */
function FieldInput({ variable, field, problem, revealed, onChange, onToggleReveal }: { variable: TemplateVariable; field: FieldState; problem: string | undefined; revealed: boolean; onChange: (patch: Partial<FieldState>) => void; onToggleReveal: () => void }) {
  const hint = variable.type === "integer" && (variable.min !== null || variable.max !== null) ? `${variable.min ?? ""}〜${variable.max ?? ""}` : undefined
  const id = `tpl-${variable.name}`
  return (
    <div className="col" style={{ gap: 6 }}>
      <label className="lbl" htmlFor={id}>{variable.label}{hint && <span className="faint" style={{ fontWeight: 400 }}> ({hint})</span>}</label>
      {variable.type === "choice" && (
        <select id={id} className="inp" value={field.value} onChange={(event) => onChange({ value: event.target.value })} aria-invalid={Boolean(problem)}>
          {variable.options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      )}
      {variable.type === "boolean" && (
        <label className="opt" style={{ cursor: "pointer", minHeight: 44 }}><input id={id} type="checkbox" checked={field.value === "true"} onChange={(event) => onChange({ value: event.target.checked ? "true" : "false" })} style={{ width: 18, height: 18 }} /><span>有効にする</span></label>
      )}
      {(variable.type === "string" || variable.type === "integer") && (
        <input id={id} className={`inp ${variable.type === "integer" ? "mono" : ""}`} inputMode={variable.type === "integer" ? "numeric" : undefined} value={field.value} placeholder={variable.default ?? ""} onChange={(event) => onChange({ value: event.target.value })} aria-invalid={Boolean(problem)} style={variable.type === "integer" ? { maxWidth: 200 } : undefined} />
      )}
      {variable.type === "password" && (
        <>
          {variable.generate && <label className="row" style={{ gap: 8, cursor: "pointer" }}><input type="checkbox" checked={field.auto} onChange={(event) => onChange({ auto: event.target.checked })} /><span>自動で作る(作成後に、画面に表示します)</span></label>}
          {!field.auto && (
            <div className="row" style={{ gap: 8, flexWrap: "nowrap" }}>
              <input id={id} className="inp mono" type={revealed ? "text" : "password"} value={field.value} onChange={(event) => onChange({ value: event.target.value })} autoComplete="new-password" aria-invalid={Boolean(problem)} />
              <button className="btn sm" type="button" onClick={onToggleReveal}>{revealed ? "隠す" : "表示"}</button>
            </div>
          )}
        </>
      )}
      {problem && <div role="alert" style={{ color: "var(--bad)", fontSize: 12.5 }}>{problem}</div>}
    </div>
  )
}

/**
 * 作成が終わったあとの、全ての変数の値の一覧(自動で作ったパスワードを、見て、コピーできるようにする)。
 *
 * @param props.template 使ったテンプレート。
 * @param props.values 全ての変数の値(自動生成を含む)。
 * @param props.revealed password を、表示しているか(変数名 → 表示)。
 * @param props.copied コピーしたばかりの変数名。
 * @param props.onToggleReveal password の表示を切り替えるとき。
 * @param props.onCopy コピーするとき。
 */
function ResultValues({ template, values, revealed, copied, onToggleReveal, onCopy }: { template: Template; values: Record<string, string>; revealed: Record<string, boolean>; copied: string; onToggleReveal: (name: string) => void; onCopy: (name: string, value: string) => void }) {
  const hasPassword = template.variables.some((variable) => variable.type === "password")
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <h2 className="h2">作成した値</h2>
      {template.variables.map((variable) => {
        const value = values[variable.name] ?? ""
        const secret = variable.type === "password"
        const shown = variable.type === "choice" ? (variable.options.find((option) => option.value === value)?.label ?? value) : secret && !revealed[variable.name] ? "●●●●●●●●●●●●" : value
        return (
          <div key={variable.name} className="row" style={{ gap: 8, flexWrap: "nowrap" }}>
            <span className="mut" style={{ minWidth: 150 }}>{variable.label}</span>
            <span className="mono grow" style={{ overflowWrap: "anywhere" }}>{shown}</span>
            {secret && <button className="btn sm" type="button" onClick={() => onToggleReveal(variable.name)}>{revealed[variable.name] ? "隠す" : "表示"}</button>}
            {secret && <button className="btn sm" type="button" onClick={() => onCopy(variable.name, value)}>{copied === variable.name ? "コピーしました" : "コピー"}</button>}
          </div>
        )
      })}
      {hasPassword && <div className="faint" style={{ fontSize: 12.5 }}>パスワードは、デプロイの「起動設定」(環境変数)でも、あとから確認できます。</div>}
    </div>
  )
}

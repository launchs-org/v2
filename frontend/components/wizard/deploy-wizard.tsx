"use client"

import { useEffect, useRef, useState } from "react"
import useSWR from "swr"
import { ApiError, api, authenticatedFetcher } from "@/lib/api"
import { KindIcon } from "@/components/kind-icon"
import { POPULAR_IMAGES, SPEC_CHOICES, guessPort, validateImage, validatePort } from "@/lib/wizard"
import type { Nav } from "@/lib/nav"
import type { Project, QuotaResponse, ServiceInfo } from "@/lib/types"

/** ウィザードの、入力のステップの名前(上の印に出す)。 */
const STEP_LABELS = ["イメージ", "スペック", "公開", "確認"]

/** 作成の工程の識別子と、画面に出す名前。 */
type TaskKey = "deploy" | "service" | "wait" | "ingress" | "rules" | "apply"
const TASK_LABELS: Record<TaskKey, string> = {
  deploy: "デプロイを作る",
  service: "ポートを公開する(Service を作る)",
  wait: "Service の準備を待つ",
  ingress: "公開 URL を取る(Ingress を作る)",
  rules: "URL と Service をつなぐ",
  apply: "公開を反映する",
}

/** 工程の状態。 */
type TaskState = "waiting" | "running" | "done" | "failed"

/** Service の準備を待つ間隔(ミリ秒)と、待つ時間の上限(ミリ秒)。 */
const POLL_MS = 2000
const POLL_LIMIT_MS = 90_000

/** 作成の途中で得た ID など(失敗したあと、続きから実行するために、取っておく)。 */
type Created = { deployId?: string; serviceId?: string; ingressId?: string; host?: string }

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
 * 最初のデプロイのウィザード。イメージ → スペック → 公開 → 確認の 4 段階で入力し、作成を順に実行する。
 * 作成は、deploy → Service → (準備を待つ)→ Ingress → ルール → 反映の順。失敗したら、その工程から、やり直せる。
 *
 * @param props.project デプロイを作るプロジェクト。
 * @param props.nav 画面の切り替え(完了後に、deploy のパネルを開く)。
 * @param props.onClose 閉じるとき。
 */
export function DeployWizard({ project, nav, onClose }: { project: Project; nav: Nav; onClose: () => void }) {
  const [step, setStep] = useState(0)
  const [image, setImage] = useState("nginx:latest")
  const [spec, setSpec] = useState<"small" | "medium" | "large">("small")
  const [expose, setExpose] = useState(true)
  const [portText, setPortText] = useState("80")
  // ポートを、利用者が自分で編集したら、イメージの変更で、上書きしない。
  const [portTouched, setPortTouched] = useState(false)
  const [phase, setPhase] = useState<"form" | "running" | "failed" | "done">("form")
  const [tasks, setTasks] = useState<{ key: TaskKey; state: TaskState }[]>([])
  const [error, setError] = useState("")
  const created = useRef<Created>({})
  const alive = useRef(true)
  const dialogRef = useRef<HTMLDivElement>(null)
  const { data: quota } = useSWR<QuotaResponse>("/api/v1/quota", authenticatedFetcher, { revalidateOnFocus: false })
  const running = phase === "running"

  // 画面を閉じたら、実行の続きを止める。開いたら、ダイアログにフォーカスを移す。
  useEffect(() => {
    alive.current = true
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    window.requestAnimationFrame(() => dialogRef.current?.focus())
    return () => {
      alive.current = false
      previous?.focus()
    }
  }, [])

  // Esc で閉じる(作成の実行中は、途中で止めないよう、閉じない)。Tab は、ダイアログの中で、ぐるぐる回す。
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && !running) {
        onClose()
        return
      }
      if (event.key !== "Tab" || !dialogRef.current) return
      const items = Array.from(dialogRef.current.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])'))
      if (!items.length) return
      const first = items[0]
      const last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [running, onClose])

  /**
   * イメージを変えたとき: 入力を反映し、ポートを、まだ触っていなければ、推測した値にする。
   *
   * @param next 新しいイメージ。
   */
  function changeImage(next: string) {
    setImage(next)
    if (!portTouched) setPortText(String(guessPort(next)))
  }

  /**
   * いまのステップの入力を検査する。
   *
   * @returns 問題の説明。問題がなければ null。
   */
  function stepProblem(): string | null {
    // イメージのステップは、イメージを検査する。
    if (step === 0) return validateImage(image.trim())
    // 公開のステップは、公開するときだけ、ポートを検査する。
    if (step === 2 && expose) return validatePort(portText)
    return null
  }

  /**
   * 工程を 1 つ実行する。成功すれば、得た ID を `created` に入れる。
   *
   * @param key 工程の識別子。
   */
  async function execute(key: TaskKey) {
    const data = created.current
    const port = Number(portText)
    const projectPath = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
    // 工程ごとに、対応する API を呼ぶ。
    if (key === "deploy") {
      const result = await api.post<{ deployId: string }>("/api/v1/deploy", { projectId: project.projectId, imageRef: image.trim(), spec })
      data.deployId = result.deployId
    } else if (key === "service") {
      const result = await api.post<ServiceInfo>(`/api/v1/deploy/${encodeURIComponent(data.deployId ?? "")}/service`, { ports: [{ name: "web", port, protocol: "tcp" }] })
      data.serviceId = result.serviceId
    } else if (key === "wait") {
      // Service が active になるまで、一定の間隔で確かめる(失敗か、時間切れなら、エラーにする)。
      const startedAt = Date.now()
      for (;;) {
        const service = await api.get<ServiceInfo>(`/api/v1/deploy/${encodeURIComponent(data.deployId ?? "")}/service`)
        if (service.status === "active") return
        if (service.status === "failed" || service.status === "deleting") throw new Error("Service の準備に失敗しました。")
        if (Date.now() - startedAt > POLL_LIMIT_MS) throw new Error("Service の準備が、90 秒以内に終わりませんでした。しばらくしてから、やり直してください。")
        await sleep(POLL_MS)
        if (!alive.current) throw new Error("中断しました。")
      }
    } else if (key === "ingress") {
      const result = await api.post<{ ingressId: string; host: string }>(`${projectPath}/ingresses`)
      data.ingressId = result.ingressId
      data.host = result.host
    } else if (key === "rules") {
      await api.put(`${projectPath}/ingresses/${encodeURIComponent(data.ingressId ?? "")}`, { rules: [{ path: "/", serviceId: data.serviceId, port }] })
    } else {
      await api.post(`${projectPath}/ingresses/${encodeURIComponent(data.ingressId ?? "")}/apply`)
    }
  }

  /**
   * 工程を、まだ終わっていないものから、順に実行する(新規の作成も、失敗後のやり直しも、同じ関数)。
   *
   * @param initial 実行する工程の一覧と状態(やり直しのときは、前回の状態)。
   */
  async function run(initial: { key: TaskKey; state: TaskState }[]) {
    setPhase("running")
    setError("")
    const list = initial.map((task) => ({ ...task }))
    setTasks(list)
    // 終わっていない工程を、上から順に実行する。
    for (const task of list) {
      if (task.state === "done") continue
      task.state = "running"
      setTasks(list.map((item) => ({ ...item })))
      try {
        await execute(task.key)
      } catch (caught) {
        // 失敗した工程に印を付けて、メッセージを出し、止める(作られたものは、消さない)。
        task.state = "failed"
        if (alive.current) {
          setTasks(list.map((item) => ({ ...item })))
          setError(caught instanceof ApiError || caught instanceof Error ? caught.message : "作成に失敗しました。")
          setPhase("failed")
        }
        return
      }
      task.state = "done"
      if (!alive.current) return
      setTasks(list.map((item) => ({ ...item })))
    }
    if (alive.current) setPhase("done")
  }

  /** 「作成する」が押されたとき: 工程の一覧を作って、実行を始める(実行中の連打は、無視する)。 */
  function start() {
    if (running) return
    created.current = {}
    const keys: TaskKey[] = expose ? ["deploy", "service", "wait", "ingress", "rules", "apply"] : ["deploy"]
    void run(keys.map((key) => ({ key, state: "waiting" as TaskState })))
  }

  /** 「やり直す」が押されたとき: 失敗した工程から、続きを実行する(失敗を、待機に戻す)。 */
  function retry() {
    if (running) return
    void run(tasks.map((task) => ({ key: task.key, state: task.state === "failed" ? ("waiting" as TaskState) : task.state })))
  }

  /** 「次へ」が押されたとき: 入力を検査して、問題がなければ進む。 */
  function next() {
    const problem = stepProblem()
    setError(problem ?? "")
    if (problem === null) setStep((current) => Math.min(3, current + 1))
  }

  /** 完了後に、「キャンバスを見る」が押されたとき: 閉じて、作った deploy のパネルを開く。 */
  function openCreated() {
    onClose()
    if (created.current.deployId) nav.openDeploy(created.current.deployId)
  }

  const specQuota = quota?.deploysBySpec?.[spec]
  const specFull = specQuota !== undefined && specQuota.used >= specQuota.limit
  const total = quota?.deploys
  const totalFull = total !== undefined && total.used >= total.limit

  return (
    <div className="modalbg" onMouseDown={(event) => { if (event.target === event.currentTarget && !running) onClose() }}>
      <style>{`.wiz-fade{animation:wizfade .18s ease-out}@keyframes wizfade{from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:none}}@media (prefers-reduced-motion:reduce){.wiz-fade{animation:none}}`}</style>
      <div ref={dialogRef} className="modal" role="dialog" aria-modal="true" aria-labelledby="wizard-title" tabIndex={-1} style={{ width: "min(760px,100%)", minHeight: 520 }}>
        <div className="row" style={{ gap: 12, flexWrap: "nowrap" }}>
          <KindIcon kind="deploy" size={36} />
          <div className="grow"><h2 id="wizard-title" className="h2" style={{ fontSize: 20 }}>はじめてのデプロイ</h2><div className="mut" style={{ fontSize: 13 }}>{project.name} に、アプリを起動します。</div></div>
          <button className="btn ico ghost" type="button" onClick={onClose} disabled={running} aria-label="閉じる"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
        </div>

        {/* ステップの印。作成を始めたら、全て完了の扱いにする。 */}
        <ol className="row" style={{ gap: 8, listStyle: "none", padding: 0, margin: 0 }} aria-label="進み具合">
          {STEP_LABELS.map((label, index) => {
            const active = phase === "form" && step === index
            const finished = phase !== "form" || step > index
            return (
              <li key={label} className="step" style={{ alignItems: "center", flex: "1 1 120px" }} aria-current={active ? "step" : undefined}>
                <span className="stepn" style={finished ? { background: "var(--accent)", color: "var(--ink)", borderColor: "var(--accent)" } : active ? { borderColor: "var(--accent)", color: "var(--accent)" } : undefined}>{finished ? "✓" : index + 1}</span>
                <span style={{ fontWeight: active ? 500 : 400, color: active ? "var(--text)" : "var(--mut)" }}>{label}</span>
              </li>
            )
          })}
        </ol>

        <div className="wiz-fade" key={`${phase}-${step}`} style={{ display: "flex", flexDirection: "column", gap: 14, flex: 1 }}>
          {/* 1. イメージ */}
          {phase === "form" && step === 0 && (
            <>
              <div className="mut">起動したいコンテナのイメージを入力します。下の候補から選ぶこともできます。</div>
              <label className="lbl">イメージ<input className="inp mono" value={image} onChange={(event) => changeImage(event.target.value)} placeholder="nginx:latest" autoFocus aria-invalid={error !== ""} /></label>
              <div className="row" style={{ gap: 8 }} role="group" aria-label="よく使うイメージ">
                {POPULAR_IMAGES.map((item) => <button key={item.image} type="button" className={`chip ${image.trim() === item.image ? "on" : ""}`} onClick={() => changeImage(item.image)}>{item.label}<span className="faint"> · {item.image}</span></button>)}
              </div>
            </>
          )}

          {/* 2. スペック */}
          {phase === "form" && step === 1 && (
            <>
              <div className="mut">コンテナの大きさを選びます。あとから変えることはできないので、迷ったら small にします。台数は 1 台で始まり、作成後に増やせます。</div>
              <div className="grid g3" role="radiogroup" aria-label="スペック">
                {SPEC_CHOICES.map((choice) => {
                  const usage = quota?.deploysBySpec?.[choice.name]
                  const remaining = usage ? Math.max(0, usage.limit - usage.used) : undefined
                  return (
                    <button key={choice.name} type="button" role="radio" aria-checked={spec === choice.name} className={`opt ${spec === choice.name ? "on" : ""}`} onClick={() => setSpec(choice.name)} style={{ flexDirection: "column", alignItems: "flex-start", gap: 4, padding: 14, minHeight: 110 }}>
                      <span className="disp" style={{ fontSize: 18, fontWeight: 500 }}>{choice.name}</span>
                      <span className="mut" style={{ fontSize: 13 }}>{choice.hint}</span>
                      {remaining !== undefined && <span className="faint" style={{ fontSize: 12 }}>あと {remaining} 個まで作れます</span>}
                    </button>
                  )
                })}
              </div>
              {(specFull || totalFull) && <div className="badge b-warn" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>{totalFull ? "デプロイの数が、上限に達しています。" : `${spec} の数が、上限に達しています。`}別のスペックを選ぶか、不要なデプロイを削除してください。</div>}
            </>
          )}

          {/* 3. 公開 */}
          {phase === "form" && step === 2 && (
            <>
              <div className="mut">外のインターネットから、アクセスできるようにするかを選びます。</div>
              <label className="opt" style={{ cursor: "pointer", minHeight: 64 }}>
                <input type="checkbox" checked={expose} onChange={(event) => { setExpose(event.target.checked); setError("") }} style={{ width: 18, height: 18 }} />
                <span className="grow"><b>外からアクセスできるようにする</b><span className="mut" style={{ display: "block", fontSize: 13 }}>Service と Ingress を作ります。</span></span>
              </label>
              {expose && (
                <>
                  <label className="lbl">コンテナが待ち受けているポート<input className="inp mono" inputMode="numeric" value={portText} onChange={(event) => { setPortText(event.target.value); setPortTouched(true) }} style={{ maxWidth: 160 }} aria-invalid={error !== ""} /></label>
                  <div className="faint" style={{ fontSize: 13 }}>公開先の URL は、作成後に決まります(例: <span className="mono">brave-blue-fox.shconn.org</span>)。</div>
                </>
              )}
            </>
          )}

          {/* 4. 確認 */}
          {phase === "form" && step === 3 && (
            <>
              <div className="mut">この内容で作成します。</div>
              <dl className="kv card pad" style={{ margin: 0 }}>
                <div><dt>イメージ</dt><dd className="mono">{image.trim()}</dd></div>
                <div><dt>スペック</dt><dd>{spec}</dd></div>
                <div><dt>公開</dt><dd>{expose ? "する(Service + Ingress)" : "しない"}</dd></div>
                {expose && <div><dt>ポート</dt><dd className="mono">{portText}</dd></div>}
              </dl>
              {(specFull || totalFull) && <div className="badge b-warn" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>上限に達しているため、作成に失敗する可能性があります。</div>}
            </>
          )}

          {/* 作成の進み具合 */}
          {phase !== "form" && (
            <>
              <div className="mut">{phase === "done" ? "作成が終わりました。" : phase === "failed" ? "途中で失敗しました。作られたものは、そのまま残っています。" : "作成しています。閉じずに、お待ちください。"}</div>
              <ul className="card" style={{ listStyle: "none", margin: 0, padding: 0 }} aria-live="polite">
                {tasks.map((task) => (
                  <li key={task.key} className="tr" style={{ ["--cols" as string]: "1fr auto" }}>
                    <span>{TASK_LABELS[task.key]}</span>
                    <span className={`badge ${task.state === "done" ? "b-ok" : task.state === "failed" ? "b-bad" : task.state === "running" ? "b-info live" : "b-dim"}`}><i />{task.state === "done" ? "完了" : task.state === "failed" ? "失敗" : task.state === "running" ? "実行中" : "待機中"}</span>
                  </li>
                ))}
              </ul>
              {phase === "done" && created.current.host && <div className="card pad row"><span className="mut">公開 URL</span><a className="lnk mono" href={`https://${created.current.host}`} target="_blank" rel="noreferrer">https://{created.current.host}</a></div>}
              {phase === "done" && created.current.host && <div className="faint" style={{ fontSize: 13 }}>反映と DNS が行き渡るまで、数分かかることがあります。</div>}
            </>
          )}

          {error && <div className="badge b-bad" role="alert" style={{ whiteSpace: "normal", borderRadius: 10, padding: "8px 12px" }}>{error}</div>}
        </div>

        {/* 下のボタン */}
        <div className="row" style={{ justifyContent: "flex-end" }}>
          {phase === "form" && <>
            <button className="btn" type="button" onClick={() => { setError(""); step === 0 ? onClose() : setStep(step - 1) }}>{step === 0 ? "キャンセル" : "戻る"}</button>
            {step < 3 ? <button className="btn primary" type="button" onClick={next}>次へ</button> : <button className="btn primary" type="button" onClick={start} disabled={running}>作成する</button>}
          </>}
          {phase === "running" && <button className="btn primary" type="button" disabled>作成しています…</button>}
          {phase === "failed" && <>
            <button className="btn" type="button" onClick={onClose}>閉じる</button>
            <button className="btn primary" type="button" onClick={retry} disabled={running}>やり直す</button>
          </>}
          {phase === "done" && <>
            <button className="btn" type="button" onClick={onClose}>閉じる</button>
            <button className="btn primary" type="button" onClick={openCreated}>キャンバスを見る</button>
          </>}
        </div>
      </div>
    </div>
  )
}

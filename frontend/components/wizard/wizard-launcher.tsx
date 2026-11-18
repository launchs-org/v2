"use client"

import { useEffect, useState } from "react"
import { Box, Layers } from "lucide-react"
import type { Nav } from "@/lib/nav"
import type { Project } from "@/lib/types"
import { DeployWizard } from "@/components/wizard/deploy-wizard"
import { TemplateWizard } from "@/components/wizard/template-wizard"

/**
 * デプロイを作る入口。最初に、「テンプレートから」と「イメージから」を選ぶ。
 * 選んだら、それぞれのウィザード(TemplateWizard、DeployWizard)に移る。
 *
 * @param props.project 作る先のプロジェクト。
 * @param props.nav 画面の切り替え。
 * @param props.onClose 閉じるとき。
 */
export function WizardLauncher({ project, nav, onClose }: { project: Project; nav: Nav; onClose: () => void }) {
  const [mode, setMode] = useState<"choose" | "template" | "image">("choose")

  // 選ぶ画面は、Esc で閉じる(各ウィザードは、それぞれ、自分で閉じる)。
  useEffect(() => {
    if (mode !== "choose") return
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose()
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [mode, onClose])

  if (mode === "template") return <TemplateWizard project={project} nav={nav} onClose={onClose} />
  if (mode === "image") return <DeployWizard project={project} nav={nav} onClose={onClose} />
  return (
    <div className="modalbg" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="launcher-title" style={{ width: "min(640px,100%)" }}>
        <div className="row"><h2 id="launcher-title" className="h2 grow" style={{ fontSize: 20 }}>どちらで作りますか?</h2><button className="btn ico ghost" type="button" onClick={onClose} aria-label="閉じる"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg></button></div>
        <div className="mut">{project.name} に、アプリを作ります。</div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 12 }}>
          <button type="button" className="opt" onClick={() => setMode("template")} autoFocus style={{ flexDirection: "column", alignItems: "flex-start", gap: 8, padding: 16, minHeight: 150, height: "auto" }}>
            <span className="kind-icon k-deploy" style={{ width: 40, height: 40 }} aria-hidden="true"><Layers size={22} /></span>
            <b style={{ fontSize: 16 }}>テンプレートから</b>
            <span className="mut" style={{ fontSize: 13 }}>PostgreSQL、WordPress など、よく使う構成を、入力欄を埋めるだけで、まとめて作ります。</span>
          </button>
          <button type="button" className="opt" onClick={() => setMode("image")} style={{ flexDirection: "column", alignItems: "flex-start", gap: 8, padding: 16, minHeight: 150, height: "auto" }}>
            <span className="kind-icon k-deploy" style={{ width: 40, height: 40 }} aria-hidden="true"><Box size={22} /></span>
            <b style={{ fontSize: 16 }}>イメージから</b>
            <span className="mut" style={{ fontSize: 13 }}>コンテナのイメージ名を指定して、1 つのアプリを起動します。</span>
          </button>
        </div>
      </div>
    </div>
  )
}

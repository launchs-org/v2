"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import { usePending } from "@/lib/pending"
import type { DeployTab, Nav, Selection, View } from "@/lib/nav"
import type { Project, ProjectListResponse, User } from "@/lib/types"
import { PendingPopover } from "@/components/pending-popover"
import { ErrorState, LoadingState } from "@/components/ui-kit"
import { ProjectsView } from "@/components/views/projects-view"
import { CanvasView } from "@/components/views/canvas-view"
import { LogsView } from "@/components/views/logs-view"
import { SettingsView } from "@/components/views/settings-view"
import { QuotaView } from "@/components/views/quota-view"
import { AccountView } from "@/components/views/account-view"

/** 画面の名前。 */
const LABEL: Record<View, string> = { projects: "プロジェクト", quota: "クォータ", account: "アカウント設定", canvas: "構成図", logs: "ログ", settings: "設定" }

/** プロジェクトの中のタブ(上のバー)。 */
const PROJECT_TABS: View[] = ["canvas", "logs", "settings"]

/** 最後に開いたプロジェクトを覚えるキー(localStorage)。 */
const LAST_PROJECT_KEY = "launchs.lastProject"

type ConsoleShellProps = { user: User; onLogout: () => Promise<void> }

/** ロゴ(紫の角丸に、A)。 */
function Logo() {
  return <svg viewBox="0 0 32 32" width="28" height="28" aria-hidden="true"><rect width="32" height="32" rx="9" fill="var(--accent)" /><path d="M9 22l7-14 7 14M12.5 17h7" stroke="#fff" strokeWidth="2.4" fill="none" strokeLinecap="round" strokeLinejoin="round" /></svg>
}

/**
 * ログイン後の外枠(Railway 風)。上のバー(ロゴ、プロジェクトの切り替え、タブ、反映待ち、ユーザー)と、残りの全てを使う本体。
 * 本体の既定は「構成図」(キャンバス)。カードを押すと、右にパネルが開く。
 *
 * @param props.user ログインしているユーザー。
 * @param props.onLogout ログアウトするとき。
 */
export function ConsoleShell({ user, onLogout }: ConsoleShellProps) {
  const [view, setView] = useState<View>("projects")
  const [projectId, setProjectId] = useState("")
  const [selection, setSelection] = useState<Selection | null>(null)
  const [deployTab, setDeployTab] = useState<DeployTab>("overview")
  const [wizardOpen, setWizardOpen] = useState(false)
  const [userMenu, setUserMenu] = useState(false)
  const [search, setSearch] = useState("")
  const [createTick, setCreateTick] = useState(0)
  const [polling, setPolling] = useState(true)
  const refreshInterval = polling ? 5000 : 0
  const { data, error, mutate } = useSWR<ProjectListResponse>("/api/v1/projects?limit=100", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true, refreshWhenHidden: false })
  const projects = data?.projects ?? []
  const project: Project | undefined = projects.find((candidate) => candidate.projectId === projectId)
  const inProject = project !== undefined && PROJECT_TABS.includes(view)

  // 最後に開いたプロジェクトを、読み込みが終わったとき、一度だけ開く(再読み込みしても、続きから)。
  useEffect(() => {
    if (!data || projectId) return
    let last = ""
    try { last = window.localStorage.getItem(LAST_PROJECT_KEY) ?? "" } catch { /* 保存できない環境では、一覧から始める */ }
    const found = data.projects.find((candidate) => candidate.projectId === last)
    if (found) { setProjectId(found.projectId); setView("canvas") }
  }, [data, projectId])

  // 画面を移る操作(各画面に渡す)。
  const nav: Nav = {
    go: (next) => { setView(next); setSelection(null); setSearch("") },
    select: (next) => { setView("canvas"); setSelection(next); if (next.kind === "deploy") setDeployTab(next.tab ?? "overview") },
    openDeploy: (id, tab = "overview") => { setView("canvas"); setSelection({ kind: "deploy", id }); setDeployTab(tab) },
    closePanel: () => setSelection(null),
    openProject: (next) => {
      setProjectId(next.projectId); setView("canvas"); setSelection(null)
      try { window.localStorage.setItem(LAST_PROJECT_KEY, next.projectId) } catch { /* 保存できなくても、動く */ }
    },
    openWizard: () => setWizardOpen(true),
  }

  return (
    <div className="app">
      <div className="shell">
        <header className="topnav">
          <button className="btn ghost" type="button" style={{ padding: "0 4px", gap: 8 }} onClick={() => nav.go("projects")} aria-label="プロジェクト一覧へ"><Logo /><span className="disp" style={{ fontSize: 18, fontWeight: 500, color: "var(--text)" }}>Launchs</span></button>
          {project && (
            <>
              <span className="faint" aria-hidden="true">/</span>
              <select className="inp" style={{ width: "auto", minWidth: 160, maxWidth: 260, minHeight: 36, fontWeight: 500 }} value={project.projectId} onChange={(event) => { const next = projects.find((candidate) => candidate.projectId === event.target.value); if (next) nav.openProject(next) }} aria-label="プロジェクトを切り替え">
                {projects.map((candidate) => <option key={candidate.projectId} value={candidate.projectId}>{candidate.name}</option>)}
              </select>
            </>
          )}
          <div className="tabs" role="tablist" style={{ marginLeft: 8 }}>
            {inProject
              ? PROJECT_TABS.map((key) => <button key={key} type="button" role="tab" aria-selected={view === key} className={`tab ${view === key ? "on" : ""}`} onClick={() => nav.go(key)}>{LABEL[key]}</button>)
              : (["projects", "quota", "account"] as View[]).map((key) => <button key={key} type="button" role="tab" aria-selected={view === key} className={`tab ${view === key ? "on" : ""}`} onClick={() => nav.go(key)}>{LABEL[key]}</button>)}
          </div>
          <div className="grow" />
          {project && <TopPending project={project} refreshInterval={refreshInterval} nav={nav} />}
          {view === "projects" && <><label className="srch" style={{ width: 240 }}><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="M20 20l-4-4" /></svg><input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="検索" aria-label="検索" /></label><button className="btn primary" type="button" onClick={() => setCreateTick((tick) => tick + 1)}>+ プロジェクトを作成</button></>}
          <button className="btn sm ghost" type="button" onClick={() => setPolling((current) => !current)} title="自動更新の切り替え"><span className={`badge ${polling ? "b-ok live" : "b-dim"}`}><i />{polling ? "自動更新" : "更新停止"}</span></button>
          <div style={{ position: "relative" }}>
            <button className="btn ico ghost" type="button" onClick={() => setUserMenu((current) => !current)} aria-label="ユーザーメニュー" aria-expanded={userMenu}><span style={{ width: 32, height: 32, borderRadius: "50%", background: "var(--accent)", color: "var(--ink)", display: "grid", placeItems: "center", fontWeight: 500 }}>{user.userName.slice(0, 1).toUpperCase()}</span></button>
            {userMenu && (
              <div className="addmenu" style={{ width: 220 }} role="menu" onClick={() => setUserMenu(false)}>
                <div style={{ padding: "6px 10px", fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis" }}>{user.userName}</div>
                <button type="button" role="menuitem" onClick={() => nav.go("projects")}><span className="t">プロジェクト一覧</span></button>
                <button type="button" role="menuitem" onClick={() => nav.go("quota")}><span className="t">クォータ</span></button>
                <button type="button" role="menuitem" onClick={() => nav.go("account")}><span className="t">アカウント設定</span></button>
                <button type="button" role="menuitem" onClick={() => void onLogout()}><span className="t" style={{ color: "var(--bad)" }}>ログアウト</span></button>
              </div>
            )}
          </div>
        </header>

        {error && !data && <div className="page"><ErrorState message="API に接続できません。" onRetry={() => void mutate()} /></div>}
        {!data && !error && <div className="page"><LoadingState /></div>}
        {data && view === "projects" && <div className="page"><ProjectsView projects={projects} refreshInterval={refreshInterval} onRefresh={mutate} nav={nav} search={search} createTick={createTick} /></div>}
        {data && view === "quota" && <div className="page"><QuotaView refreshInterval={refreshInterval} /></div>}
        {data && view === "account" && <div className="page"><AccountView user={user} refreshInterval={refreshInterval} onLogout={onLogout} /></div>}
        {data && !project && PROJECT_TABS.includes(view) && <div className="page"><ProjectsView projects={projects} refreshInterval={refreshInterval} onRefresh={mutate} nav={nav} search="" createTick={0} /></div>}
        {project && view === "canvas" && <CanvasView project={project} refreshInterval={refreshInterval} nav={nav} selection={selection} deployTab={deployTab} onDeployTabChange={setDeployTab} wizardOpen={wizardOpen} onWizardClose={() => setWizardOpen(false)} />}
        {project && view === "logs" && <div className="page fill"><LogsView project={project} refreshInterval={refreshInterval} nav={nav} /></div>}
        {project && view === "settings" && <div className="page"><SettingsView project={project} refreshInterval={refreshInterval} nav={nav} onChanged={() => void mutate()} /></div>}
      </div>
    </div>
  )
}

/** 上のバーの、反映待ち(プロジェクトごとに集計する)。 */
function TopPending({ project, refreshInterval, nav }: { project: Project; refreshInterval: number; nav: Nav }) {
  const pending = usePending(project, refreshInterval)
  return <PendingPopover pending={pending} nav={nav} />
}

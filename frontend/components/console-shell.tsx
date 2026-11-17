"use client"

import { useState } from "react"
import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import { usePending } from "@/lib/pending"
import type { DeployTab, Nav, View } from "@/lib/nav"
import type { Project, ProjectListResponse, User } from "@/lib/types"
import { Badge, ErrorState, LoadingState } from "@/components/ui-kit"
import { ProjectsView } from "@/components/views/projects-view"
import { OverviewView } from "@/components/views/overview-view"
import { DeploysView } from "@/components/views/deploys-view"
import { BuildsView } from "@/components/views/builds-view"
import { ServicesView } from "@/components/views/services-view"
import { IngressesView } from "@/components/views/ingresses-view"
import { VolumesView } from "@/components/views/volumes-view"
import { SettingsView } from "@/components/views/settings-view"
import { QuotaView } from "@/components/views/quota-view"
import { AccountView } from "@/components/views/account-view"

/** アイコンの形(モックと同じ)。 */
const ICON: Record<string, string> = {
  overview: "M3 3h8v8H3zM13 3h8v5h-8zM13 11h8v10h-8zM3 14h8v7H3z",
  deploys: "M12 3l8 4.5v9L12 21l-8-4.5v-9zM12 12l8-4.5M12 12v9M12 12L4 7.5",
  services: "M12 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM5 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM19 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM12 9v3M12 12l-5.5 3M12 12l5.5 3",
  ingresses: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18",
  volumes: "M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3",
  builds: "M14 4l6 6-3 3-6-6zM11 7L4 14l-1 5 5-1 7-7",
  settings: "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19 12a7 7 0 0 0-.1-1.2l2-1.5-2-3.4-2.3.9a7 7 0 0 0-2-1.2L14 3h-4l-.4 2.6a7 7 0 0 0-2 1.2l-2.3-.9-2 3.4 2 1.5A7 7 0 0 0 5 12c0 .4 0 .8.1 1.2l-2 1.5 2 3.4 2.3-.9a7 7 0 0 0 2 1.2L10 21h4l.4-2.6a7 7 0 0 0 2-1.2l2.3.9 2-3.4-2-1.5c.1-.4.1-.8.1-1.2z",
}

const LABEL: Record<View, string> = { projects: "プロジェクト", quota: "クォータ", account: "アカウント設定", overview: "概要", deploys: "デプロイ", builds: "ビルド", services: "サービス", ingresses: "Ingress", volumes: "ボリューム", settings: "設定" }

/** 上のバーの「作成」ボタン(画面ごと)。無い画面は、出さない。 */
const CREATE_LABEL: Partial<Record<View, string>> = { projects: "プロジェクトを作成", deploys: "デプロイを作成", ingresses: "Ingress を作成", volumes: "ボリュームを作成" }

/** 検索欄を出す画面。 */
const SEARCHABLE: View[] = ["projects", "deploys", "services", "volumes"]

type ConsoleShellProps = { user: User; onLogout: () => Promise<void> }

/**
 * ログイン後の外枠(モックのデザイン)。左にナビゲーション、上にバー、中に画面。
 * プロジェクトを選ぶ前の画面(プロジェクト一覧、クォータ、アカウント)は、サイドバー無しで、上のタブで切り替える。
 *
 * @param props.user ログインしているユーザー。
 * @param props.onLogout ログアウトするとき。
 */
export function ConsoleShell({ user, onLogout }: ConsoleShellProps) {
  const [view, setView] = useState<View>("projects")
  const [projectId, setProjectId] = useState("")
  const [deployId, setDeployId] = useState("")
  const [deployTab, setDeployTab] = useState<DeployTab>("overview")
  const [search, setSearch] = useState("")
  const [createTick, setCreateTick] = useState(0)
  const [polling, setPolling] = useState(true)
  const refreshInterval = polling ? 5000 : 0
  const { data, error, mutate } = useSWR<ProjectListResponse>("/api/v1/projects?limit=100", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true, refreshWhenHidden: false })
  const projects = data?.projects ?? []
  const project: Project | undefined = projects.find((candidate) => candidate.projectId === projectId)
  const hasSide = view !== "projects" && view !== "quota" && view !== "account" && project !== undefined

  // 画面を移る操作(各画面に渡す)。
  const nav: Nav = {
    go: (next) => { setView(next); setDeployId(""); setSearch("") },
    openDeploy: (id, tab = "overview") => { setView("deploys"); setDeployId(id); setDeployTab(tab); setSearch("") },
    closeDeploy: () => setDeployId(""),
    openProject: (next) => { setProjectId(next.projectId); setView("overview"); setDeployId(""); setSearch("") },
  }

  return (
    <div className="app">
      <div style={{ display: "flex", flexWrap: "wrap", height: "100vh", minHeight: 720, width: "100%" }}>
        {hasSide && project && <Side project={project} projects={projects} view={view} user={user} refreshInterval={refreshInterval} nav={nav} onLogout={onLogout} />}
        <main className="main">
          <div className="top">
            {hasSide ? <div className="mono mut" style={{ fontSize: 13 }}>{project?.name} / {LABEL[view]}{view === "deploys" && deployId ? ` / ${deployId}` : ""}</div> : (
              <>
                <div className="row" style={{ gap: 10, marginRight: 12 }}>
                  <svg viewBox="0 0 32 32" width="30" height="30" aria-hidden="true"><rect width="32" height="32" rx="9" fill="var(--accent)" /><path d="M9 22l7-14 7 14M12.5 17h7" stroke="#fff" strokeWidth="2.4" fill="none" strokeLinecap="round" strokeLinejoin="round" /></svg>
                  <span className="disp" style={{ fontSize: 19, fontWeight: 500 }}>Launchs</span>
                </div>
                <div className="tabs" role="tablist" style={{ borderBottom: 0, alignSelf: "stretch" }}>
                  {(["projects", "quota", "account"] as View[]).map((key) => <button key={key} type="button" role="tab" aria-selected={view === key} className={`tab ${view === key ? "on" : ""}`} onClick={() => nav.go(key)}>{LABEL[key]}</button>)}
                </div>
              </>
            )}
            {SEARCHABLE.includes(view) && !deployId && (
              <label className="srch"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="M20 20l-4-4" /></svg><input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="検索" aria-label="検索" /></label>
            )}
            <div className="grow" />
            {hasSide && project && <PendingTop project={project} refreshInterval={refreshInterval} onClick={() => nav.go("overview")} />}
            <button className="btn sm ghost" type="button" onClick={() => setPolling((current) => !current)} title="自動更新の切り替え"><span className={`badge ${polling ? "b-ok live" : "b-dim"}`}><i />{polling ? "自動更新 5秒" : "更新停止"}</span></button>
            {CREATE_LABEL[view] && !deployId && (view === "projects" || project) && <button className="btn primary" type="button" onClick={() => setCreateTick((tick) => tick + 1)}>+ {CREATE_LABEL[view]}</button>}
            {!hasSide && <div className="row" style={{ gap: 8 }}><span className="mut">{user.userName}</span><button className="btn sm" type="button" onClick={() => void onLogout()}>ログアウト</button></div>}
          </div>
          <div className="page">
            {error && !data && <ErrorState message="API に接続できません。" onRetry={() => void mutate()} />}
            {!data && !error && <LoadingState />}
            {data && view === "projects" && <ProjectsView projects={projects} refreshInterval={refreshInterval} onRefresh={mutate} nav={nav} search={search} createTick={createTick} />}
            {data && view === "quota" && <QuotaView refreshInterval={refreshInterval} />}
            {data && view === "account" && <AccountView user={user} refreshInterval={refreshInterval} onLogout={onLogout} />}
            {data && !project && view !== "projects" && view !== "quota" && view !== "account" && <ProjectsView projects={projects} refreshInterval={refreshInterval} onRefresh={mutate} nav={nav} search="" createTick={0} />}
            {project && view === "overview" && <OverviewView project={project} refreshInterval={refreshInterval} nav={nav} search={search} createTick={createTick} />}
            {project && view === "deploys" && <DeploysView project={project} refreshInterval={refreshInterval} nav={nav} search={search} createTick={createTick} deployId={deployId} tab={deployTab} onTabChange={setDeployTab} />}
            {project && view === "builds" && <BuildsView project={project} refreshInterval={refreshInterval} nav={nav} search={search} createTick={createTick} />}
            {project && view === "services" && <ServicesView project={project} refreshInterval={refreshInterval} nav={nav} search={search} createTick={createTick} />}
            {project && view === "ingresses" && <IngressesView project={project} refreshInterval={refreshInterval} nav={nav} search={search} createTick={createTick} />}
            {project && view === "volumes" && <VolumesView project={project} refreshInterval={refreshInterval} nav={nav} search={search} createTick={createTick} />}
            {project && view === "settings" && <SettingsView project={project} refreshInterval={refreshInterval} nav={nav} search={search} createTick={createTick} onChanged={() => void mutate()} />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** 上のバーの「反映待ち N 件」(押すと、概要の Apply センターへ)。 */
function PendingTop({ project, refreshInterval, onClick }: { project: Project; refreshInterval: number; onClick: () => void }) {
  const { items } = usePending(project, refreshInterval)
  if (!items.length) return null
  return <button className="btn sm" type="button" onClick={onClick}><span className="pdot" />反映待ち {items.length} 件</button>
}

/** 左のサイドバー(プロジェクトを開いているとき)。 */
function Side({ project, projects, view, user, refreshInterval, nav, onLogout }: { project: Project; projects: Project[]; view: View; user: User; refreshInterval: number; nav: Nav; onLogout: () => Promise<void> }) {
  const { items } = usePending(project, refreshInterval)
  const count = (kinds: string[]) => items.filter((item) => kinds.includes(item.kind)).length
  const groups: { title: string; items: { key: View; count: number }[] }[] = [
    { title: "", items: [{ key: "overview", count: items.length }] },
    { title: "アプリケーション", items: [{ key: "deploys", count: count(["起動設定", "マウント"]) }, { key: "builds", count: 0 }] },
    { title: "ネットワーク", items: [{ key: "services", count: count(["Service"]) }, { key: "ingresses", count: count(["Ingress"]) }] },
    { title: "ストレージ", items: [{ key: "volumes", count: count(["ボリューム"]) }] },
    { title: "プロジェクト", items: [{ key: "settings", count: 0 }] },
  ]
  return (
    <aside className="side">
      <div className="row" style={{ gap: 10, padding: "2px 6px" }}>
        <svg viewBox="0 0 32 32" width="30" height="30" aria-hidden="true"><rect width="32" height="32" rx="9" fill="var(--accent)" /><path d="M9 22l7-14 7 14M12.5 17h7" stroke="#fff" strokeWidth="2.4" fill="none" strokeLinecap="round" strokeLinejoin="round" /></svg>
        <span className="disp" style={{ fontSize: 19, fontWeight: 500 }}>Launchs</span>
      </div>
      <button className="btn" type="button" style={{ justifyContent: "flex-start" }} onClick={() => nav.go("projects")}><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M15 6l-6 6 6 6" /></svg>プロジェクト一覧へ戻る</button>
      <div className="card" style={{ padding: "10px 12px", background: "var(--bg)", display: "flex", flexDirection: "column", gap: 8 }}>
        <div className="row" style={{ justifyContent: "space-between", gap: 8, flexWrap: "nowrap" }}><span className="faint" style={{ fontSize: 12, fontWeight: 500 }}>プロジェクト</span><Badge status={project.status} /></div>
        <select className="inp" value={project.projectId} onChange={(event) => { const next = projects.find((candidate) => candidate.projectId === event.target.value); if (next) nav.openProject(next) }} aria-label="プロジェクトを切り替え">
          {projects.map((candidate) => <option key={candidate.projectId} value={candidate.projectId}>{candidate.name}</option>)}
        </select>
      </div>
      <nav style={{ display: "flex", flexDirection: "column", gap: 6 }} aria-label="メニュー">
        {groups.map((group) => (
          <div key={group.title || "top"} style={{ display: "flex", flexDirection: "column", gap: 2 }}>
            {group.title && <div className="sec">{group.title}</div>}
            {group.items.map((item) => (
              <button key={item.key} type="button" className={`navb ${view === item.key ? "on" : ""}`} onClick={() => nav.go(item.key)} aria-current={view === item.key ? "page" : undefined}>
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={ICON[item.key]} /></svg>
                <span>{LABEL[item.key]}</span>
                {item.count > 0 && <span className="cnt">{item.count}</span>}
              </button>
            ))}
          </div>
        ))}
      </nav>
      <div className="card" style={{ marginTop: "auto", padding: 12, display: "flex", alignItems: "center", gap: 10, background: "var(--panel2)" }}>
        <div style={{ width: 34, height: 34, borderRadius: "50%", background: "var(--accent)", color: "var(--ink)", display: "grid", placeItems: "center", fontWeight: 500 }}>{user.userName.slice(0, 1).toUpperCase()}</div>
        <div style={{ minWidth: 0, flex: 1, fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis" }}>{user.userName}</div>
        <button className="btn ico ghost" type="button" onClick={() => void onLogout()} aria-label="ログアウト"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" /></svg></button>
      </div>
    </aside>
  )
}

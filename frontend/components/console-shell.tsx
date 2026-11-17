"use client"

import { useState } from "react"
import useSWR from "swr"
import {
  Activity, Box, ChevronDown, CircleUserRound, Database, FileCode2, Globe2, Layers3,
  LayoutDashboard, LogOut, Pause, Play, RefreshCw, TerminalSquare,
} from "lucide-react"
import { authenticatedFetcher } from "@/lib/api"
import type { Project, ProjectListResponse, User } from "@/lib/types"
import { StatusPill } from "@/components/ui-kit"
import { OverviewView } from "@/components/views/overview-view"
import { ProjectsView } from "@/components/views/projects-view"
import { DeploymentsView } from "@/components/views/deployments-view"
import { VolumesView } from "@/components/views/volumes-view"
import { IngressesView } from "@/components/views/ingresses-view"
import { LogsView } from "@/components/views/logs-view"
import { AccountView } from "@/components/views/account-view"

type Section = "overview" | "projects" | "deployments" | "volumes" | "ingresses" | "logs" | "account"

type ConsoleShellProps = { user: User; onLogout: () => Promise<void> }
const navItems: { key: Section; label: string; icon: typeof LayoutDashboard }[] = [
  { key: "overview", label: "概要", icon: LayoutDashboard },
  { key: "projects", label: "プロジェクト", icon: Layers3 },
  { key: "deployments", label: "デプロイ", icon: Box },
  { key: "volumes", label: "ボリューム", icon: Database },
  { key: "ingresses", label: "Ingress", icon: Globe2 },
  { key: "logs", label: "ログ", icon: FileCode2 },
  { key: "account", label: "アカウント", icon: CircleUserRound },
]
const sectionLabels: Record<Section, string> = { overview: "概要", projects: "プロジェクト", deployments: "デプロイ", volumes: "ボリューム", ingresses: "Ingress", logs: "ログ", account: "アカウント設定" }

export function ConsoleShell({ user, onLogout }: ConsoleShellProps) {
  const [section, setSection] = useState<Section>("overview")
  const [selectedProjectId, setSelectedProjectId] = useState("")
  const [projectMenuOpen, setProjectMenuOpen] = useState(false)
  const [polling, setPolling] = useState(true)
  const { data, error, mutate } = useSWR<ProjectListResponse>("/api/v1/projects?limit=100", authenticatedFetcher, {
    refreshInterval: polling ? 15_000 : 0,
    revalidateOnFocus: true,
    refreshWhenHidden: false,
  })
  const projects = data?.projects ?? []
  const selectedProject = projects.find((project) => project.projectId === selectedProjectId) ?? projects[0]
  const activeSection: Section = !projects.length && data && section !== "projects" && section !== "account" ? "projects" : section
  const refreshInterval = polling ? 5000 : 0
  const onProjectChange = (project: Project) => {
    setSelectedProjectId(project.projectId)
    setProjectMenuOpen(false)
  }

  return (
    <div className="console-layout">
      <aside className="sidebar">
        <div className="sidebar-brand"><span className="brand-mark"><TerminalSquare size={17} /></span><span>launchs</span></div>
        <div className="workspace-label">WORKSPACE</div>
        <div className="project-menu-wrap">
          <button className="project-switcher" type="button" onClick={() => setProjectMenuOpen((open) => !open)} aria-expanded={projectMenuOpen} aria-haspopup="listbox">
            <span className="project-symbol"><Layers3 size={14} /></span>
            <span className="project-switcher-copy"><span className="project-switcher-name">{selectedProject?.name || "プロジェクトを選択"}</span><span className="project-switcher-meta">{selectedProject ? "プロジェクト" : `${projects.length} projects`}</span></span>
            <ChevronDown size={14} />
          </button>
          {projectMenuOpen && <div className="project-popover" role="listbox" aria-label="プロジェクトを選択">
            {projects.map((project) => <button className={`project-option ${selectedProject?.projectId === project.projectId ? "selected" : ""}`} key={project.projectId} type="button" role="option" aria-selected={selectedProject?.projectId === project.projectId} onClick={() => onProjectChange(project)}><span className="project-option-mark"><Layers3 size={13} /></span><span className="project-option-name">{project.name}</span><StatusPill status={project.status} /></button>)}
            <button className="project-option project-option-manage" type="button" onClick={() => { setSection("projects"); setProjectMenuOpen(false) }}><Layers3 size={13} />プロジェクトを管理</button>
          </div>}
        </div>
        <nav className="side-nav" aria-label="メインメニュー">
          {navItems.map(({ key, label, icon: Icon }) => <button className={`nav-item ${activeSection === key ? "active" : ""}`} key={key} type="button" onClick={() => setSection(key)} aria-current={activeSection === key ? "page" : undefined}><Icon size={16} strokeWidth={1.8} /><span>{label}</span></button>)}
        </nav>
        <div className="sidebar-bottom">
          {/* 本物の API につないでいる(live)ときは、見せかけのデータの表示を出さない。 */}
          {process.env.NEXT_PUBLIC_LAUNCHS_API_MODE !== "live" && (
            <div className="cluster-card">
              <div className="cluster-top"><span>DEMO MODE</span><span className="cluster-live"><span className="live-dot" />MOCK DATA</span></div>
              <div className="cluster-name">Local mock control plane</div>
              <div className="cluster-sub">変更はこのプレビュー内のみ</div>
            </div>
          )}
          <div className="user-chip-row">
            <button className="user-chip" type="button" onClick={() => setSection("account")}>
              <span className="user-avatar">{user.userName.slice(0, 1).toUpperCase()}</span>
              <span className="user-chip-copy"><span className="user-chip-name">{user.userName}</span><span className="user-chip-role">{user.role === "admin" ? "Administrator" : "Workspace member"}</span></span>
            </button>
            <button className="icon-button user-logout" type="button" aria-label="ログアウト" title="ログアウト" onClick={() => void onLogout()}><LogOut size={14} /></button>
          </div>
        </div>
      </aside>

      <div className="main-area">
        <header className="topbar">
          <div className="breadcrumb"><span>Launchs</span><span>/</span><strong>{selectedProject?.name || (activeSection === "projects" ? "Projects" : sectionLabels[activeSection])}</strong><span>/</span><strong>{sectionLabels[activeSection]}</strong></div>
          <div className="topbar-actions">
            <span className="refresh-state"><span className="live-dot" />{polling ? "自動更新 · 5秒" : "自動更新を停止中"}</span>
            <button className={`refresh-toggle ${polling ? "" : "is-paused"}`} type="button" onClick={() => setPolling((current) => !current)} title={polling ? "自動更新を一時停止" : "自動更新を再開"} aria-label={polling ? "自動更新を一時停止" : "自動更新を再開"}>{polling ? <Pause size={14} /> : <Play size={14} />}</button>
            <button className="refresh-toggle" type="button" onClick={() => void mutate()} title="プロジェクト一覧を更新" aria-label="プロジェクト一覧を更新"><RefreshCw size={14} /></button>
          </div>
        </header>
        <main id="main-content" className="content">
          {error && !data ? <div className="card"><div className="api-error">APIに接続できません。Launchs API が同一オリジンで利用可能かご確認ください。<button className="text-link" type="button" onClick={() => void mutate()}>再試行</button></div></div> : null}
          {activeSection === "overview" && selectedProject && <OverviewView project={selectedProject} refreshInterval={refreshInterval} onNavigate={setSection} />}
          {activeSection === "projects" && <ProjectsView projects={projects} loading={!data && !error} onRefresh={mutate} onSelect={(project) => { onProjectChange(project); setSection("overview") }} />}
          {activeSection === "deployments" && selectedProject && <DeploymentsView project={selectedProject} refreshInterval={refreshInterval} />}
          {activeSection === "volumes" && selectedProject && <VolumesView project={selectedProject} refreshInterval={refreshInterval} />}
          {activeSection === "ingresses" && selectedProject && <IngressesView project={selectedProject} refreshInterval={refreshInterval} />}
          {activeSection === "logs" && selectedProject && <LogsView project={selectedProject} refreshInterval={refreshInterval} />}
          {activeSection === "account" && <AccountView user={user} refreshInterval={refreshInterval} onLogout={onLogout} />}
          {!selectedProject && activeSection !== "projects" && activeSection !== "account" && !error && <div className="card project-empty"><div className="empty-state"><div className="empty-icon"><Activity size={19} /></div><strong>まずはプロジェクトを作成しましょう</strong><p>プロジェクトを作成すると、デプロイやストレージなどのリソースをまとめて管理できます。</p><button className="btn btn-primary" type="button" onClick={() => setSection("projects")}>プロジェクトを作成</button></div></div>}
        </main>
      </div>
    </div>
  )
}

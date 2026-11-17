"use client"

import { useState } from "react"
import { api } from "@/lib/api"
import type { DeployTab, ProjectViewProps } from "@/lib/nav"
import { usePending, type PendingItem } from "@/lib/pending"
import { Badge, LoadingState, ToastNotice } from "@/components/ui-kit"

/** 反映待ちの種類から、開く deploy のタブを決める。 */
const TAB_OF_KIND: Record<string, DeployTab> = { 起動設定: "config", マウント: "mounts", Service: "service" }

/**
 * 概要の画面(Apply センター)。リソースの数、反映待ちの変更(破棄と反映)、最近のデプロイを見せる。
 *
 * @param props 共通の引数(project、refreshInterval、nav)。
 */
export function OverviewView({ project, refreshInterval, nav }: ProjectViewProps) {
  const { items, loading, refresh, deployDetails, ingressDetails } = usePending(project, refreshInterval)
  const [busyKeys, setBusyKeys] = useState<string[]>([])
  const [toast, setToast] = useState<{ message: string; error: boolean } | null>(null)
  const running = deployDetails.filter((deploy) => deploy.status === "active").length
  const ruleCount = ingressDetails.reduce((total, ingress) => total + ingress.rules.length, 0)
  const recent = [...deployDetails].sort((first, second) => second.createdAt.localeCompare(first.createdAt)).slice(0, 3)
  const stats = [
    { label: "デプロイ", value: deployDetails.length, sub: `${running} 稼働中` },
    { label: "Ingress", value: ingressDetails.length, sub: `${ruleCount} ルール` },
    { label: "反映待ち", value: items.length, sub: "件" },
  ]

  /**
   * 反映待ちの変更に、操作(適用か破棄)を送る。
   *
   * @param targets 対象の変更。
   * @param action 適用か破棄。
   */
  async function act(targets: PendingItem[], action: "apply" | "discard") {
    // 操作中の印を付けてから、順に送る(失敗したら、止めて、知らせる)。
    setBusyKeys((current) => [...current, ...targets.map((item) => item.key)])
    try {
      for (const item of targets) await api.post(action === "apply" ? item.applyPath : item.discardPath)
      setToast({ message: action === "apply" ? "反映を開始しました" : "変更を破棄しました", error: false })
    } catch (caught) {
      setToast({ message: caught instanceof Error ? caught.message : "操作できませんでした", error: true })
    } finally {
      await refresh()
      setBusyKeys((current) => current.filter((key) => !targets.some((item) => item.key === key)))
    }
  }

  return (
    <>
      <div className="row">
        <div className="grow">
          <div className="row"><h1 className="h1 disp">{project.name}</h1><Badge status={project.status} /></div>
          <div className="faint mono" style={{ marginTop: 2 }}>{project.projectId}</div>
        </div>
      </div>
      <div className="card" style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(170px,1fr))" }}>
        {stats.map((stat) => (
          <div key={stat.label} style={{ padding: "12px 18px", borderRight: "1px solid var(--line)" }}>
            <div className="faint" style={{ fontSize: 12.5 }}>{stat.label}</div>
            <div style={{ fontSize: 22, fontWeight: 500 }}>{stat.value} <span className="mut" style={{ fontSize: 12.5, fontWeight: 400 }}>{stat.sub}</span></div>
          </div>
        ))}
      </div>
      <div className="split" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(460px,1fr))" }}>
        <div className="card">
          <div className="row pad" style={{ paddingBottom: 4 }}>
            <h2 className="h2 grow">反映待ちの変更</h2>
            {items.length > 0 && <button className="btn warn sm" type="button" onClick={() => void act(items, "apply")} disabled={busyKeys.length > 0}>すべて反映</button>}
          </div>
          {loading && <LoadingState />}
          {!loading && items.length === 0 && <div className="mut pad" style={{ paddingTop: 8, borderTop: "1px solid var(--line)" }}>反映待ちの変更はありません。</div>}
          <div className="tscroll"><div className="tinner" style={{ minWidth: 520 }}>
            {items.map((item) => (
              <div key={item.key} className="tr" style={{ "--cols": "92px 1fr auto" } as React.CSSProperties}>
                <span className="tag" style={{ justifySelf: "start" }}>{item.kind}</span>
                <div style={{ minWidth: 0 }}>
                  {item.deployId ? <button className="lnk mono" type="button" onClick={() => nav.openDeploy(item.deployId!, TAB_OF_KIND[item.kind] ?? "overview")}>{item.target}</button> : <button className="lnk mono" type="button" onClick={() => nav.go(item.kind === "Ingress" ? "ingresses" : "volumes")}>{item.target}</button>}
                  <div className="mut" style={{ fontSize: 12.5 }}>{item.sub}</div>
                </div>
                <div className="row" style={{ gap: 8, flexWrap: "nowrap" }}>
                  <button className="btn sm" type="button" onClick={() => void act([item], "discard")} disabled={busyKeys.includes(item.key)}>破棄</button>
                  <button className="btn sm warn" type="button" onClick={() => void act([item], "apply")} disabled={busyKeys.includes(item.key)}>反映</button>
                </div>
              </div>
            ))}
          </div></div>
        </div>
        <div className="col">
          <div className="card">
            <div className="pad" style={{ paddingBottom: 8 }}><h2 className="h2">最近のデプロイ</h2></div>
            {recent.length === 0 && <div className="tr mut" style={{ "--cols": "1fr" } as React.CSSProperties}>デプロイはまだありません。</div>}
            {recent.map((deploy) => (
              <div key={deploy.deployId} className="tr" style={{ "--cols": "1fr auto" } as React.CSSProperties}>
                <button className="lnk" type="button" onClick={() => nav.openDeploy(deploy.deployId)}><span className="mono">{deploy.deployId.slice(0, 16)}</span> <span className="faint" style={{ fontWeight: 400 }}>{deploy.imageRef.split("/").pop()}</span></button>
                <Badge status={deploy.status} />
              </div>
            ))}
          </div>
          <div className="card">
            <div className="pad" style={{ paddingBottom: 8 }}><h2 className="h2">最近のビルド</h2></div>
            <div className="tr mut" style={{ "--cols": "1fr" } as React.CSSProperties}>ビルドは未対応です。</div>
          </div>
        </div>
      </div>
      {toast && <ToastNotice message={toast.message} type={toast.error ? "error" : "success"} onDismiss={() => setToast(null)} />}
    </>
  )
}

"use client"

import type { ProjectViewProps } from "@/lib/nav"
import { usePending } from "@/lib/pending"
import { Badge, EmptyState, LoadingState } from "@/components/ui-kit"

/** 表の列幅(モックと同じ)。 */
const COLS = { "--cols": "1.3fr 1fr 1.2fr 2fr 130px 120px" } as React.CSSProperties

/**
 * サービスの一覧の画面(各 deploy の service をまとめて見せる)。行を押すと、その deploy の Service のタブを開く。
 *
 * @param props 共通の引数(project、refreshInterval、nav、search)。
 */
export function ServicesView({ project, refreshInterval, nav, search }: ProjectViewProps) {
  const { deployDetails, loading } = usePending(project, refreshInterval)
  const keyword = search.trim().toLowerCase()
  // service を持つ deploy だけを、検索の文字で絞る。
  const rows = deployDetails.filter((deploy) => deploy.service !== null).filter((deploy) => !keyword || [deploy.service?.serviceId, deploy.deployId, deploy.imageRef, deploy.service?.dnsName].some((text) => (text ?? "").toLowerCase().includes(keyword)))
  return (
    <>
      <div><h1 className="h1 disp">サービス</h1></div>
      {loading && <LoadingState />}
      {!loading && rows.length === 0 && <EmptyState title="Service はまだありません" description="デプロイの「Service」タブで、ポートを公開すると作れます。" />}
      {rows.length > 0 && (
        <div className="card tscroll"><div className="tinner" style={{ minWidth: 900 }}>
          <div className="tr th" style={COLS}><span>Service</span><span>デプロイ</span><span>ポート</span><span>DNS</span><span>ClusterIP</span><span>状態</span></div>
          {rows.map((deploy) => {
            const service = deploy.service!
            return (
              <button key={service.serviceId} type="button" className="tr rowb" style={COLS} onClick={() => nav.openDeploy(deploy.deployId, "service")}>
                <span className="mono" style={{ fontWeight: 500, color: "var(--accent)", wordBreak: "break-all" }}>{service.serviceId.slice(0, 20)}</span>
                <span className="mono mut" style={{ wordBreak: "break-all" }}>{deploy.deployId.slice(0, 16)}</span>
                <span className="mono">{service.ports.map((port) => `${port.port}/${port.protocol}`).join(", ")}</span>
                <span className="mono mut" style={{ wordBreak: "break-all", fontSize: 11.5 }}>{service.dnsName ?? "—"}</span>
                <span className="mono">{service.clusterIP ?? "—"}</span>
                <span style={{ display: "flex", gap: 8, alignItems: "center" }}><Badge status={service.status} />{service.pending_ports && <span className="pdot" title="未反映の変更あり" />}</span>
              </button>
            )
          })}
        </div></div>
      )}
    </>
  )
}

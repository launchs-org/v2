"use client"

import type { CanvasModel } from "@/lib/canvas-layout"
import type { Selection } from "@/lib/nav"
import { KindIcon } from "@/components/kind-icon"
import { Badge } from "@/components/ui-kit"

/** 種類の表示名。 */
const KIND_LABEL: Record<string, string> = { deploy: "デプロイ", service: "Service", ingress: "Ingress", volume: "ボリューム" }

/** 一覧に出す種類の並び順(構成図の左から右の順)。 */
const KIND_ORDER = ["ingress", "service", "deploy", "volume"]

/**
 * プロジェクトのリソースの表(スマホ向け。構成図の代わり)。1 行が 1 つのリソースで、押すと詳細を開く。
 *
 * @param props.model 構成図と同じ元のデータ(インターネットのカードは出さない)。
 * @param props.onSelect 行を押したとき。
 * @param props.selection 開いているリソース(強調に使う)。
 */
export function ResourceTable({ model, onSelect, selection }: { model: CanvasModel; onSelect: (selection: Selection) => void; selection: Selection | null }) {
  // インターネットのカードを除き、種類の順(同じ種類の中は、元の順)に並べる。
  const rows = model.nodes
    .filter((node) => node.kind !== "internet")
    .sort((first, second) => KIND_ORDER.indexOf(first.kind) - KIND_ORDER.indexOf(second.kind))

  /** 行を押したとき、種類に応じた詳細を開く(service は、持ち主の deploy の「service」タブ)。 */
  function open(node: (typeof rows)[number]) {
    onSelect(node.kind === "deploy" ? { kind: "deploy", id: node.id } : node.kind === "service" ? { kind: "deploy", id: node.deployId ?? "", tab: "service" } : node.kind === "ingress" ? { kind: "ingress", id: node.id } : { kind: "volume", id: node.id })
  }

  return (
    <div className="card rtable">
      {rows.map((node) => {
        const selected = selection !== null && selection.id === (node.kind === "service" ? node.deployId : node.id)
        return (
          <button key={node.key} type="button" className={`rtable-row ${selected ? "on" : ""}`} onClick={() => open(node)}>
            <KindIcon kind={node.kind as "deploy" | "service" | "ingress" | "volume"} size={32} />
            <span className="rtable-main">
              <span className="rtable-name">{node.title}{node.pending && <span className="pdot" title="反映待ちの変更があります" style={{ marginLeft: 8, display: "inline-block" }} />}</span>
              <span className="rtable-sub mono">{KIND_LABEL[node.kind]} · {node.subtitle}{node.extra ? ` · ${node.extra.replace(/^https?:\/\//, "")}` : ""}</span>
            </span>
            <Badge status={node.status} />
          </button>
        )
      })}
    </div>
  )
}

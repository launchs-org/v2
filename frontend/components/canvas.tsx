"use client"

import "@xyflow/react/dist/style.css"
import { useMemo } from "react"
import { BaseEdge, Background, BackgroundVariant, Controls, EdgeLabelRenderer, getBezierPath, Handle, Position, ReactFlow, type Edge, type EdgeProps, type Node, type NodeProps } from "@xyflow/react"
import { KindIcon } from "@/components/kind-icon"
import { Badge } from "@/components/ui-kit"
import { selectionKey, type CanvasModel, type NodeInfo } from "@/lib/canvas-layout"
import type { Selection } from "@/lib/nav"

/** ノードに持たせるデータ(React Flow の型の制約で、index signature を付ける)。 */
type NodeData = NodeInfo & { selected: boolean; highlighted: boolean; dimmed: boolean; highlightTag: string | null; onTagClick: (tag: string) => void; [key: string]: unknown }

/** カードに出すタグの最大の数(残りは「+N」)。 */
const MAX_CARD_TAGS = 2

/** 点が、線の端から端まで流れる時間(秒)。 */
const FLOW_SECONDS = 3
/** 3 つの点の、開始のずれ(秒)。流れる時間を、3 等分する。 */
const FLOW_OFFSETS = [0, FLOW_SECONDS / 3, (FLOW_SECONDS * 2) / 3]

/** 動きを減らす設定か(その場合、線の点は流さない)。 */
function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches
}

/**
 * カード1枚(リソースの色つきアイコン、名前、補足、状態、未適用の印)。
 *
 * @param props React Flow が渡す、ノードのデータ。
 */
function ResourceNode({ data }: NodeProps<Node<NodeData>>) {
  return (
    <div className={`rf-node ${data.selected ? "sel" : ""} ${data.highlighted ? "hl" : ""} ${data.dimmed ? "dim" : ""}`} role="button" tabIndex={0} aria-label={`${data.title} の詳細を開く`}>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      {data.pending && <span className="pend" title="反映待ちの変更があります" />}
      <div className="row" style={{ gap: 10, flexWrap: "nowrap" }}>
        <KindIcon kind={data.kind === "internet" ? "ingress" : data.kind} size={36} />
        <div style={{ minWidth: 0, flex: 1 }}>
          <div className="nm" title={data.title}>{data.title}</div>
          <div className="sub">{data.subtitle}</div>
        </div>
      </div>
      <div className="foot">
        <Badge status={data.status} />

          {/* 選んだタグは、必ず見えるように、先頭に出す。押すと、同じタグのカードが強調される(もう一度押すと解除)。 */}
          {[...(data.tags ?? [])].sort((first, second) => Number(second === data.highlightTag) - Number(first === data.highlightTag)).slice(0, MAX_CARD_TAGS).map((tag) => <button key={tag} type="button" className={`tagchip nodrag nopan ${tag === data.highlightTag ? "on" : ""}`} title={`タグ ${tag}(押すと、同じタグを強調)`} onClick={(event) => { event.stopPropagation(); data.onTagClick(tag) }}>{tag}</button>)}
          {(data.tags ?? []).length > MAX_CARD_TAGS && <span className="tagmore" title={(data.tags ?? []).slice(MAX_CARD_TAGS).join("、")}>+{(data.tags ?? []).length - MAX_CARD_TAGS}</span>}
        {data.extra && <span className="url mono">{data.href ? <a href={data.href} target="_blank" rel="noreferrer" onClick={(event) => event.stopPropagation()}>{data.extra.replace("https://", "")}</a> : data.extra}</span>}
      </div>
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </div>
  )
}

/**
 * 線(薄い曲線)。稼働中のつながりには、小さな点が流れる。線の中央に、path などの言葉を置く。
 *
 * @param props React Flow が渡す、線のデータ。
 */
function FlowEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data }: EdgeProps<Edge<{ label: string; live: boolean }>>) {
  const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition })
  const animate = Boolean(data?.live) && !prefersReducedMotion()
  return (
    <>
      <BaseEdge id={id} path={path} />
      {/* 流れる点: 3 つを、等間隔にずらして流す(1 つが端で消えても、次が続く)。端では、フェードイン・アウトして、急に戻って見えないようにする。 */}
      {animate && FLOW_OFFSETS.map((offset) => (
        <circle key={offset} r="3.5" className="flow-dot" opacity="0">
          <animateMotion dur={`${FLOW_SECONDS}s`} begin={`${offset}s`} repeatCount="indefinite" path={path} keyPoints="0;1" keyTimes="0;1" calcMode="linear" />
          <animate attributeName="opacity" dur={`${FLOW_SECONDS}s`} begin={`${offset}s`} repeatCount="indefinite" values="0;1;1;0" keyTimes="0;0.15;0.85;1" />
        </circle>
      ))}
      {data?.label && (
        <EdgeLabelRenderer>
          <span className="mono" style={{ position: "absolute", transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, padding: "1px 6px", borderRadius: 6, background: "#fff", border: "1px solid var(--line)", fontSize: 11, color: "var(--mut)", pointerEvents: "none" }}>{data.label}</span>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

/**
 * 「インターネット」のカード(地球のマーク)。リソースではないので、押しても何も開かない。ingress へ、線がつながる。
 *
 * @param props React Flow が渡す、ノードのデータ。
 */
function InternetNode({ data }: NodeProps<Node<NodeData>>) {
  return (
    <div className="rf-node rf-internet" aria-label="インターネット(外からのアクセスの入口)">
      <div className="row" style={{ gap: 10, flexWrap: "nowrap" }}>
        <span className="internet-mark" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18" /></svg>
        </span>
        <div style={{ minWidth: 0, flex: 1 }}>
          <div className="nm">{data.title}</div>
          <div className="sub">{data.subtitle}</div>
        </div>
      </div>
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </div>
  )
}

const nodeTypes = { resource: ResourceNode, internet: InternetNode }
const edgeTypes = { flow: FlowEdge }

/**
 * キャンバス(パンとズーム。カードは自動配置)。カードを押すと、`onSelect` が呼ばれる。
 *
 * @param props.model カードと線。
 * @param props.selection いま、右のパネルで開いているカード。
 * @param props.onSelect カードを押したとき。
 * @param props.onBackgroundClick 何もない所を押したとき(パネルを閉じる)。
 * @param props.highlightTag 強調するタグ(そのタグの deploy のカードを強調し、他は薄くする)。無ければ null。
 * @param props.onTagClick カードのタグを押したとき(強調の切り替え)。
 */
export function Canvas({ model, selection, onSelect, onBackgroundClick, highlightTag, onTagClick }: { model: CanvasModel; selection: Selection | null; onSelect: (selection: Selection) => void; onBackgroundClick: () => void; highlightTag: string | null; onTagClick: (tag: string) => void }) {
  // deploy のパネルを「service」のタブで開いているときは、service のカードを強調し、それ以外は deploy のカードを強調する。
  const selectedKey = selection?.kind === "deploy" && selection.tab === "service" ? `service:${model.nodes.find((node) => node.kind === "service" && node.deployId === selection.id)?.id ?? ""}` : selectionKey(selection)
  // モデルから、React Flow のノードと線を作る(選択の変化でも、作り直す)。
  const nodes = useMemo<Node<NodeData>[]>(() => model.nodes.map((node) => ({ id: node.key, type: node.kind === "internet" ? "internet" : "resource", position: { x: node.x, y: node.y }, draggable: false, data: { ...node, selected: node.key === selectedKey, highlighted: Boolean(highlightTag) && (node.tags ?? []).includes(highlightTag as string), dimmed: Boolean(highlightTag) && !(node.tags ?? []).includes(highlightTag as string), highlightTag, onTagClick } })), [model.nodes, selectedKey, highlightTag, onTagClick])
  const edges = useMemo<Edge[]>(() => model.edges.map((edge) => ({ id: edge.id, source: edge.source, target: edge.target, type: "flow", className: edge.pending ? "pending" : edge.live ? "live" : "", data: { label: edge.label, live: edge.live } })), [model.edges])
  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      fitView
      fitViewOptions={{ padding: 0.35, maxZoom: 1 }}
      minZoom={0.3}
      maxZoom={1.6}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable={false}
      proOptions={{ hideAttribution: true }}
      onNodeClick={(_event, node) => { const data = node.data as NodeData; if (data.kind === "internet") return; onSelect(data.kind === "deploy" ? { kind: "deploy", id: data.id } : data.kind === "service" ? { kind: "deploy", id: data.deployId ?? "", tab: "service" } : data.kind === "ingress" ? { kind: "ingress", id: data.id } : { kind: "volume", id: data.id }) }}
      onPaneClick={onBackgroundClick}
    >
      <Background variant={BackgroundVariant.Dots} gap={22} size={1.4} color="#D5D7E0" />
      <Controls showInteractive={false} position="bottom-left" style={{ marginLeft: 16, marginBottom: 16 }} />
    </ReactFlow>
  )
}

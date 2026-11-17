"use client"

import "@xyflow/react/dist/style.css"
import { useMemo } from "react"
import { BaseEdge, Background, BackgroundVariant, Controls, EdgeLabelRenderer, getBezierPath, Handle, Position, ReactFlow, type Edge, type EdgeProps, type Node, type NodeProps } from "@xyflow/react"
import { KindIcon } from "@/components/kind-icon"
import { Badge } from "@/components/ui-kit"
import { selectionKey, type CanvasModel, type NodeInfo } from "@/lib/canvas-layout"
import type { Selection } from "@/lib/nav"

/** ノードに持たせるデータ(React Flow の型の制約で、index signature を付ける)。 */
type NodeData = NodeInfo & { selected: boolean; [key: string]: unknown }

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
    <div className={`rf-node ${data.selected ? "sel" : ""}`} role="button" tabIndex={0} aria-label={`${data.title} の詳細を開く`}>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      {data.pending && <span className="pend" title="反映待ちの変更があります" />}
      <div className="row" style={{ gap: 10, flexWrap: "nowrap" }}>
        <KindIcon kind={data.kind} size={36} />
        <div style={{ minWidth: 0, flex: 1 }}>
          <div className="nm" title={data.title}>{data.title}</div>
          <div className="sub">{data.subtitle}</div>
        </div>
      </div>
      <div className="foot">
        <Badge status={data.status} />
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
      {animate && <circle r="3.5" className="flow-dot"><animateMotion dur="2.6s" repeatCount="indefinite" path={path} /></circle>}
      {data?.label && (
        <EdgeLabelRenderer>
          <span className="mono" style={{ position: "absolute", transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, padding: "1px 6px", borderRadius: 6, background: "#fff", border: "1px solid var(--line)", fontSize: 11, color: "var(--mut)", pointerEvents: "none" }}>{data.label}</span>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

const nodeTypes = { resource: ResourceNode }
const edgeTypes = { flow: FlowEdge }

/**
 * キャンバス(パンとズーム。カードは自動配置)。カードを押すと、`onSelect` が呼ばれる。
 *
 * @param props.model カードと線。
 * @param props.selection いま、右のパネルで開いているカード。
 * @param props.onSelect カードを押したとき。
 * @param props.onBackgroundClick 何もない所を押したとき(パネルを閉じる)。
 */
export function Canvas({ model, selection, onSelect, onBackgroundClick }: { model: CanvasModel; selection: Selection | null; onSelect: (selection: Selection) => void; onBackgroundClick: () => void }) {
  const selectedKey = selectionKey(selection)
  // モデルから、React Flow のノードと線を作る(選択の変化でも、作り直す)。
  const nodes = useMemo<Node<NodeData>[]>(() => model.nodes.map((node) => ({ id: node.key, type: "resource", position: { x: node.x, y: node.y }, draggable: false, data: { ...node, selected: node.key === selectedKey } })), [model.nodes, selectedKey])
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
      onNodeClick={(_event, node) => { const data = node.data as NodeData; onSelect(data.kind === "deploy" ? { kind: "deploy", id: data.id } : data.kind === "ingress" ? { kind: "ingress", id: data.id } : { kind: "volume", id: data.id }) }}
      onPaneClick={onBackgroundClick}
    >
      <Background variant={BackgroundVariant.Dots} gap={22} size={1.4} color="#D5D7E0" />
      <Controls showInteractive={false} position="bottom-left" style={{ marginLeft: 16, marginBottom: 16 }} />
    </ReactFlow>
  )
}

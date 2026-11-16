// プロジェクトの構成(ingress → deploy → volume)を、キャンバスに置くための、位置と線の計算。
// 3つの列(ingress、deploy、volume)に、同じ大きさのカードを縦に並べ、つながりを線(曲線)で結ぶ。
import type { DeployService, Ingress, Mount, Volume } from "../api/types";

/** カードの幅(px)。 */
export const NODE_WIDTH = 248;
/** カードの高さ(px)。 */
export const NODE_HEIGHT = 84;
/** カードの縦の間隔(px)。 */
export const NODE_GAP = 20;
/** 列の間隔(カードの左端から、次の列のカードの左端まで。px)。 */
export const COLUMN_STEP = 360;
/** キャンバスの余白(px)。 */
export const CANVAS_PADDING = 24;
/** 列の見出しの高さ(px)。 */
export const HEADER_HEIGHT = 32;

/** カードの種類。 */
export type NodeKind = "ingress" | "deploy" | "volume";

/** グラフに渡す、deploy 1つの情報。 */
export interface GraphDeploy {
  deployId: string;
  imageRef: string;
  status: string;
  replicas: number;
  service: DeployService | null;
  mounts: Mount[];
  pendingMounts: Mount[] | null;
}

/** キャンバスのカード1つ。 */
export interface GraphNode {
  /** 種類と ID を合わせた、一意のキー(例: "deploy:deploy-1")。 */
  key: string;
  kind: NodeKind;
  id: string;
  x: number;
  y: number;
}

/** カードをつなぐ線1つ。 */
export interface GraphEdge {
  key: string;
  from: string;
  to: string;
  /** 線に添える言葉(path や mountPath)。 */
  label: string;
  /** 適用前の変更で、足されるつながりなら true(点線で描く)。 */
  pending: boolean;
}

/** グラフの全体。 */
export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  width: number;
  height: number;
}

/**
 * カードのキーを作る。
 *
 * @param kind カードの種類。
 * @param id リソースの ID。
 * @returns 「種類:ID」の形のキー。
 */
export function nodeKey(kind: NodeKind, id: string): string {
  return `${kind}:${id}`;
}

/**
 * 列(0 ingress、1 deploy、2 volume)の、行番号から、カードの左上の位置を求める。
 *
 * @param column 列の番号。
 * @param row 行の番号。
 * @returns 左上の x と y。
 */
function position(column: number, row: number): { x: number; y: number } {
  // 余白と、見出しの分を空けて、行ごとに積む。
  return { x: CANVAS_PADDING + column * COLUMN_STEP, y: CANVAS_PADDING + HEADER_HEIGHT + row * (NODE_HEIGHT + NODE_GAP) };
}

/**
 * プロジェクトの、ingress・deploy・volume から、カードの位置と、つながりの線を作る。
 *
 * @param input ingress の詳細、deploy の情報、volume の一覧。
 * @returns カード、線、キャンバスの大きさ。
 */
export function buildGraph(input: { ingresses: Ingress[]; deploys: GraphDeploy[]; volumes: Volume[] }): Graph {
  const nodes: GraphNode[] = [];
  // 列ごとに、上から順に、カードを置く。
  input.ingresses.forEach((ingress, row) => nodes.push({ key: nodeKey("ingress", ingress.ingressId), kind: "ingress", id: ingress.ingressId, ...position(0, row) }));
  input.deploys.forEach((deploy, row) => nodes.push({ key: nodeKey("deploy", deploy.deployId), kind: "deploy", id: deploy.deployId, ...position(1, row) }));
  input.volumes.forEach((volume, row) => nodes.push({ key: nodeKey("volume", volume.volumeId), kind: "volume", id: volume.volumeId, ...position(2, row) }));

  const edges: GraphEdge[] = [];
  // serviceId から、そのサービスを持つ deploy を引く表。
  const deployByService = new Map<string, GraphDeploy>();
  for (const deploy of input.deploys) {
    if (deploy.service !== null) {
      deployByService.set(deploy.service.serviceId, deploy);
    }
  }
  // ingress の rules(いまの分と、適用前の分)から、ingress → deploy の線を作る。
  for (const ingress of input.ingresses) {
    const seen = new Set<string>();
    const current = ingress.rules.map((rule) => ({ rule, pending: false }));
    const added = (ingress.pending_rules ?? []).map((rule) => ({ rule, pending: true }));
    for (const entry of [...current, ...added]) {
      const target = deployByService.get(entry.rule.serviceId);
      // 流す先の deploy が無い rules(サービスが消えた)は、線にできない。
      if (target === undefined) {
        continue;
      }
      // 同じ組(ingress、deploy、path)は、1本にまとめる。いまある分を優先する。
      const key = `${ingress.ingressId}>${target.deployId}>${entry.rule.path}`;
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      edges.push({ key, from: nodeKey("ingress", ingress.ingressId), to: nodeKey("deploy", target.deployId), label: entry.rule.path, pending: entry.pending });
    }
  }
  // deploy のマウント(いまの分と、適用前の分)から、deploy → volume の線を作る。
  const knownVolumes = new Set(input.volumes.map((volume) => volume.volumeId));
  for (const deploy of input.deploys) {
    const seen = new Set<string>();
    const current = deploy.mounts.map((mount) => ({ mount, pending: false }));
    const added = (deploy.pendingMounts ?? []).map((mount) => ({ mount, pending: true }));
    for (const entry of [...current, ...added]) {
      // 一覧に無い volume(削除済み)は、線にできない。
      if (!knownVolumes.has(entry.mount.volumeId)) {
        continue;
      }
      const key = `${deploy.deployId}>${entry.mount.volumeId}>${entry.mount.mountPath}`;
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      edges.push({ key, from: nodeKey("deploy", deploy.deployId), to: nodeKey("volume", entry.mount.volumeId), label: entry.mount.mountPath, pending: entry.pending });
    }
  }
  // キャンバスの大きさは、一番長い列に合わせる。
  const rows = Math.max(1, input.ingresses.length, input.deploys.length, input.volumes.length);
  return {
    nodes,
    edges,
    width: CANVAS_PADDING * 2 + 2 * COLUMN_STEP + NODE_WIDTH,
    height: CANVAS_PADDING * 2 + HEADER_HEIGHT + rows * NODE_HEIGHT + (rows - 1) * NODE_GAP,
  };
}

/**
 * 2枚のカードを結ぶ、曲線(SVG の path)を作る。左のカードの右端の中央から、右のカードの左端の中央へ。
 *
 * @param from 線の元のカード。
 * @param to 線の先のカード。
 * @returns SVG の path の d 属性。
 */
export function edgePath(from: GraphNode, to: GraphNode): string {
  // 元は右端の中央、先は左端の中央。
  const startX = from.x + NODE_WIDTH;
  const startY = from.y + NODE_HEIGHT / 2;
  const endX = to.x;
  const endY = to.y + NODE_HEIGHT / 2;
  // 制御点を、横に離して、なめらかな曲線にする。
  const reach = Math.max(40, Math.abs(endX - startX) / 2);
  return `M ${startX} ${startY} C ${startX + reach} ${startY}, ${endX - reach} ${endY}, ${endX} ${endY}`;
}

/**
 * 線の真ん中の位置を求める(線の言葉を置くため)。
 *
 * @param from 線の元のカード。
 * @param to 線の先のカード。
 * @returns 中央の x と y。
 */
export function edgeMidpoint(from: GraphNode, to: GraphNode): { x: number; y: number } {
  return { x: (from.x + NODE_WIDTH + to.x) / 2, y: (from.y + to.y) / 2 + NODE_HEIGHT / 2 };
}

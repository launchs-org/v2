// キャンバスのカード(ノード)と線(エッジ)を、プロジェクトのリソースから作る。位置は、自動で決める(3 つの列)。
import type { Selection } from "@/lib/nav"
import type { DeployDetail, IngressDetail, Volume } from "@/lib/types"

/** 列の間隔と、行の間隔(px)。 */
const COLUMN_STEP = 340
const ROW_STEP = 168

/** カードに出す情報。 */
export type NodeInfo = {
  kind: "deploy" | "ingress" | "volume" | "service"
  id: string
  /** service のカードのとき、持ち主の deploy の ID(押すと、その deploy のパネルを開く)。 */
  deployId?: string
  title: string
  subtitle: string
  /** API の状態(バッジに使う)。 */
  status: string
  /** 補足の1行(公開 URL、ポート、レプリカなど)。 */
  extra?: string
  /** 補足が URL のとき、そのリンク。 */
  href?: string
  /** 未適用の変更があるか(橙の点)。 */
  pending: boolean
}

/** 線の情報。 */
export type EdgeInfo = {
  id: string
  source: string
  target: string
  /** 線に添える言葉(path、マウント先)。 */
  label: string
  /** 稼働中のつながり(点が流れる)。 */
  live: boolean
  /** 未適用で、足されるつながり(橙の点線)。 */
  pending: boolean
}

/** キャンバスの内容。 */
export type CanvasModel = {
  nodes: (NodeInfo & { key: string; x: number; y: number })[]
  edges: EdgeInfo[]
}

/**
 * カードのキーを作る。
 *
 * @param kind 種類。
 * @param id リソースの ID。
 * @returns 「種類:ID」の形のキー。
 */
export function nodeKey(kind: string, id: string): string {
  return `${kind}:${id}`
}

/**
 * 選択(右のパネルの対象)から、カードのキーを作る。
 *
 * @param selection 選択。無ければ null。
 * @returns カードのキー。選択が無ければ null。
 */
export function selectionKey(selection: Selection | null): string | null {
  return selection === null ? null : nodeKey(selection.kind, selection.id)
}

/**
 * イメージの参照から、カードに出す短い名前を作る(レジストリのホストを除く)。
 *
 * @param imageRef イメージの参照。
 * @returns 最後の「/」より後ろ。
 */
export function shortImage(imageRef: string): string {
  const slash = imageRef.lastIndexOf("/")
  return slash === -1 ? imageRef : imageRef.slice(slash + 1)
}

/**
 * プロジェクトの deploy・ingress・volume から、キャンバスのカードと線を作る。
 * 列: 左が ingress、中央が deploy、右が volume。同じ列は、上から順に並べる。
 *
 * @param input deploy の詳細、ingress の詳細、volume の一覧。
 * @returns カード(位置つき)と線。
 */
export function buildCanvas(input: { deploys: DeployDetail[]; ingresses: IngressDetail[]; volumes: Volume[] }): CanvasModel {
  const nodes: CanvasModel["nodes"] = []
  const edges: EdgeInfo[] = []

  // ingress のカード(左の列)。
  input.ingresses.forEach((ingress, row) => {
    nodes.push({ key: nodeKey("ingress", ingress.ingressId), kind: "ingress", id: ingress.ingressId, title: ingress.host, subtitle: `ルール ${ingress.rules.length} 件`, status: ingress.status, extra: `https://${ingress.host}`, href: `https://${ingress.host}`, pending: ingress.pending_rules !== null, x: 0, y: row * ROW_STEP })
  })

  // deploy のカード(中央の列)。
  const deployByService = new Map<string, DeployDetail>()
  input.deploys.forEach((deploy, row) => {
    if (deploy.service) deployByService.set(deploy.service.serviceId, deploy)
    const ports = deploy.service?.ports.map((port) => `:${port.port}`).join(" ") ?? ""
    const running = deploy.pods.filter((pod) => pod.status === "running").length
    const pending = deploy.config.pending_env !== null || deploy.config.pending_command !== null || deploy.config.pending_args !== null || deploy.volumes.pending_mounts !== null || Boolean(deploy.service?.pending_ports)
    nodes.push({ key: nodeKey("deploy", deploy.deployId), kind: "deploy", id: deploy.deployId, title: shortImage(deploy.imageRef), subtitle: `${deploy.spec} · ${running}/${deploy.replicas} 台${ports ? ` · ${ports}` : ""}`, status: deploy.status, pending, x: COLUMN_STEP * 2, y: row * ROW_STEP })
  })

  // service のカード(ingress と deploy の間の列)。service を持つ deploy ごとに、1枚。
  let serviceRow = 0
  const serviceNodeByDeploy = new Map<string, string>()
  for (const deploy of input.deploys) {
    const service = deploy.service
    if (!service) continue
    const ports = service.ports.map((port) => `${port.port}/${port.protocol}`).join("  ")
    nodes.push({ key: nodeKey("service", service.serviceId), kind: "service", id: service.serviceId, deployId: deploy.deployId, title: service.dnsName ? service.dnsName.split(".")[0]! : service.serviceId, subtitle: ports || "ポートなし", status: service.status, extra: service.clusterIP ?? undefined, pending: Boolean(service.pending_ports), x: COLUMN_STEP, y: serviceRow * ROW_STEP })
    serviceNodeByDeploy.set(deploy.deployId, nodeKey("service", service.serviceId))
    serviceRow += 1
  }

  // volume のカード(右の列)。
  input.volumes.forEach((volume, row) => {
    nodes.push({ key: nodeKey("volume", volume.volumeId), kind: "volume", id: volume.volumeId, title: volume.name, subtitle: `${volume.sizeMB.toLocaleString("ja-JP")} MB${volume.pending_sizeMB !== null ? ` → ${volume.pending_sizeMB.toLocaleString("ja-JP")} MB` : ""}`, status: volume.status, pending: volume.pending_sizeMB !== null, x: COLUMN_STEP * 3, y: row * ROW_STEP })
  })

  // ingress → deploy の線(いまのルールと、未適用のルール)。
  const volumeIds = new Set(input.volumes.map((volume) => volume.volumeId))
  for (const ingress of input.ingresses) {
    const seen = new Set<string>()
    const entries = [...ingress.rules.map((rule) => ({ rule, pending: false })), ...(ingress.pending_rules ?? []).map((rule) => ({ rule, pending: true }))]
    for (const entry of entries) {
      const target = deployByService.get(entry.rule.serviceId)
      if (!target) continue
      const key = `${ingress.ingressId}>${target.deployId}>${entry.rule.path}`
      if (seen.has(key)) continue
      seen.add(key)
      edges.push({ id: key, source: nodeKey("ingress", ingress.ingressId), target: serviceNodeByDeploy.get(target.deployId) ?? nodeKey("deploy", target.deployId), label: entry.rule.path, live: !entry.pending && ingress.status === "active" && target.status === "active", pending: entry.pending })
    }
  }

  // service → deploy の線(ポートを添える)。
  for (const deploy of input.deploys) {
    const serviceNode = serviceNodeByDeploy.get(deploy.deployId)
    if (!serviceNode || !deploy.service) continue
    edges.push({ id: `${serviceNode}>${deploy.deployId}`, source: serviceNode, target: nodeKey("deploy", deploy.deployId), label: "", live: deploy.service.status === "active" && deploy.status === "active", pending: false })
  }

  // deploy → volume の線(いまのマウントと、未適用のマウント)。
  for (const deploy of input.deploys) {
    const seen = new Set<string>()
    const entries = [...deploy.volumes.mounts.map((mount) => ({ mount, pending: false })), ...(deploy.volumes.pending_mounts ?? []).map((mount) => ({ mount, pending: true }))]
    for (const entry of entries) {
      if (!volumeIds.has(entry.mount.volumeId)) continue
      const key = `${deploy.deployId}>${entry.mount.volumeId}>${entry.mount.mountPath}`
      if (seen.has(key)) continue
      seen.add(key)
      edges.push({ id: key, source: nodeKey("deploy", deploy.deployId), target: nodeKey("volume", entry.mount.volumeId), label: entry.mount.mountPath, live: !entry.pending && deploy.status === "active", pending: entry.pending })
    }
  }
  return { nodes, edges }
}

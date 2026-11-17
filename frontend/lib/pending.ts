// プロジェクトの「未適用の変更(pending)」を、全リソースから集める(概要の Apply センターと、ナビの件数に使う)。
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { DeployDetail, DeployListResponse, IngressDetail, IngressListResponse, Project, Volume, VolumeListResponse } from "@/lib/types"

/** 未適用の変更の種類。 */
export type PendingKind = "起動設定" | "マウント" | "Service" | "Ingress" | "ボリューム"

/** 未適用の変更1件。 */
export type PendingItem = {
  /** 一意のキー。 */
  key: string
  kind: PendingKind
  /** 対象の名前(deploy の ID、ホスト名、ボリューム名)。 */
  target: string
  /** 補足(何が変わるか)。 */
  sub: string
  /** 適用の API のパス。 */
  applyPath: string
  /** 破棄の API のパス。 */
  discardPath: string
  /** どの deploy の変更か(deploy の変更だけ)。 */
  deployId?: string
}

/** 未適用の変更の集計。 */
export type PendingSummary = {
  items: PendingItem[]
  /** 読み込み中か(最初の取得が終わっていないか)。 */
  loading: boolean
  /** 取り直す。 */
  refresh: () => Promise<unknown>
  /** deploy の詳細(ID で引ける。概要の「最近のデプロイ」などに使い回す)。 */
  deployDetails: DeployDetail[]
  ingressDetails: IngressDetail[]
  /** volume の一覧(キャンバスのカードに使う)。 */
  volumes: Volume[]
}

/**
 * プロジェクトの、deploy・ingress・volume を取り、未適用の変更を集める。
 *
 * @param project プロジェクト。
 * @param refreshInterval 自動更新の間隔(ミリ秒。0 なら止める)。
 * @returns 未適用の変更の一覧と、取り直す関数。
 */
export function usePending(project: Project, refreshInterval: number): PendingSummary {
  const base = `/api/v1/projects/${encodeURIComponent(project.projectId)}`
  // 一覧が変わると、詳細の問い合わせのキー(ID の並び)も変わる。そのとき、前のデータを残して、取れるまで、画面を空にしない。
  const options = { refreshInterval, revalidateOnFocus: true, keepPreviousData: true }
  const { data: deployList } = useSWR<DeployListResponse>(`${base}/deploys?limit=100`, authenticatedFetcher, options)
  const { data: ingressList } = useSWR<IngressListResponse>(`${base}/ingresses?limit=100`, authenticatedFetcher, options)
  const { data: volumeList, mutate: refreshVolumes } = useSWR<VolumeListResponse>(`${base}/volumes?limit=100`, authenticatedFetcher, options)
  // deploy と ingress は、未適用の変更が詳細にしか無いので、詳細を、まとめて取る。
  const deployKey = deployList ? `pending-deploys:${project.projectId}:${deployList.deploys.map((deploy) => deploy.deployId).join(",")}` : null
  const { data: deployDetails, mutate: refreshDeploys } = useSWR<DeployDetail[]>(deployKey, () => Promise.all((deployList?.deploys ?? []).map((deploy) => api.get<DeployDetail>(`/api/v1/deploy/${encodeURIComponent(deploy.deployId)}`))), options)
  const ingressKey = ingressList ? `pending-ingresses:${project.projectId}:${ingressList.ingresses.map((ingress) => ingress.ingressId).join(",")}` : null
  const { data: ingressDetails, mutate: refreshIngresses } = useSWR<IngressDetail[]>(ingressKey, () => Promise.all((ingressList?.ingresses ?? []).map((ingress) => api.get<IngressDetail>(`${base}/ingresses/${encodeURIComponent(ingress.ingressId)}`))), options)

  const items: PendingItem[] = []
  for (const deploy of deployDetails ?? []) {
    const path = `/api/v1/deploy/${encodeURIComponent(deploy.deployId)}`
    const config = deploy.config
    if (config.pending_env !== null || config.pending_command !== null || config.pending_args !== null) items.push({ key: `config:${deploy.deployId}`, kind: "起動設定", target: deploy.deployId, sub: "環境変数・コマンド・引数(反映すると Pod が再作成されます)", applyPath: `${path}/config/apply`, discardPath: `${path}/config/discard`, deployId: deploy.deployId })
    if (deploy.volumes.pending_mounts !== null) items.push({ key: `mounts:${deploy.deployId}`, kind: "マウント", target: deploy.deployId, sub: `${deploy.volumes.pending_mounts.length} 件のマウント(反映すると Pod が再作成されます)`, applyPath: `${path}/mounts/apply`, discardPath: `${path}/mounts/discard`, deployId: deploy.deployId })
    if (deploy.service?.pending_ports) items.push({ key: `service:${deploy.deployId}`, kind: "Service", target: deploy.service.serviceId, sub: `${deploy.service.pending_ports.length} ポート(Pod は再起動しません)`, applyPath: `${path}/service/apply`, discardPath: `${path}/service/discard`, deployId: deploy.deployId })
  }
  for (const ingress of ingressDetails ?? []) {
    if (ingress.pending_rules !== null) items.push({ key: `ingress:${ingress.ingressId}`, kind: "Ingress", target: ingress.host, sub: `${ingress.pending_rules.length} ルール`, applyPath: `${base}/ingresses/${encodeURIComponent(ingress.ingressId)}/apply`, discardPath: `${base}/ingresses/${encodeURIComponent(ingress.ingressId)}/discard` })
  }
  for (const volume of volumeList?.volumes ?? []) {
    if (volume.pending_sizeMB !== null) items.push({ key: `volume:${volume.volumeId}`, kind: "ボリューム", target: volume.name, sub: `${volume.sizeMB} MB → ${volume.pending_sizeMB} MB`, applyPath: `${base}/volumes/${encodeURIComponent(volume.volumeId)}/apply`, discardPath: `${base}/volumes/${encodeURIComponent(volume.volumeId)}/discard` })
  }
  // 「読み込み中」は、最初のデータが届くまでだけ。キーが変わって、取り直している間は、前のデータを見せ続ける(keepPreviousData)。
  const loading = !deployList || !ingressList || !volumeList || (deployKey !== null && !deployDetails) || (ingressKey !== null && !ingressDetails)
  const refresh = () => Promise.all([refreshDeploys(), refreshIngresses(), refreshVolumes()])
  return { items, loading, refresh, deployDetails: deployDetails ?? [], ingressDetails: ingressDetails ?? [], volumes: volumeList?.volumes ?? [] }
}

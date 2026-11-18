export type User = {
  userId: string
  userName: string
  status: "active" | "banned"
  role: "user" | "admin"
  createdAt: string
}

export type AuthConfig = {
  turnstile: { enabled: boolean; siteKey?: string }
}

export type Project = {
  projectId: string
  name: string
  status: "active" | "deleting" | "delete_failed"
}

export type ProjectListResponse = { projects: Project[]; nextCursor: string | null }

export type DeployListItem = {
  deployId: string
  status: "pending" | "creating" | "active" | "applying" | "scaling" | "failed" | "deleting" | "delete_failed"
  spec: "small" | "medium" | "large"
  replicas: number
  imageRef: string
  createdAt: string
}

export type DeployListResponse = { deploys: DeployListItem[]; nextCursor: string | null }
export type PodInfo = { podId: string; status: string }
export type LogPodInfo = { podId: string; running: boolean; startedAt: string; endedAt: string | null }
export type ServicePort = { name: string; port: number; protocol: "tcp" | "udp" }
export type ServiceInfo = {
  serviceId: string
  ports: ServicePort[]
  pending_ports: ServicePort[] | null
  status: "pending" | "active" | "failed" | "deleting"
  dnsName: string | null
  clusterIP: string | null
}
export type MountItem = { volumeId: string; mountPath: string }
export type DeployConfig = {
  env: Record<string, string>
  pending_env: Record<string, string> | null
  command: string | null
  pending_command: string | null
  args: string[] | null
  pending_args: string[] | null
}
/** deploy が、共有の ConfigMap の 1 ファイルを、1 つの path にマウントする指定(docs/endpoints/deploys.md)。 */
export type ConfigMapMount = {
  configMapId: string
  /** 固定している版。保存するとき、省略(0)なら、サーバーが決める。 */
  revision: number
  key: string
  path: string
  /** 権限(8 進数 4 桁の文字列)。 */
  mode: string
  /** ConfigMap の名前(GET .../configmap-mounts だけ)。 */
  configMapName?: string
  /** ConfigMap の今の最新の版(GET .../configmap-mounts だけ)。 */
  latestRevision?: number
}
export type ConfigMapMountsState = { mounts: ConfigMapMount[]; pending_mounts: ConfigMapMount[] | null }

export type DeployDetail = DeployListItem & {
  projectId: string
  pods: PodInfo[]
  logPods: LogPodInfo[]
  config: DeployConfig
  volumes: { mounts: MountItem[]; pending_mounts: MountItem[] | null }
  /** 共有の ConfigMap のマウント(名前と最新の版は含まない)。 */
  configmapMounts?: ConfigMapMountsState
  service: ServiceInfo | null
}
export type DeployLogsResponse = { logs: string; nextSince: number }

export type Volume = {
  volumeId: string
  name: string
  status: "active" | "creating" | "applying" | "deleting"
  sizeMB: number
  pending_sizeMB: number | null
  createdAt?: string
  /** マウントしている deploy の ID(詳細のみ)。 */
  mountedBy?: string[]
}
export type VolumeListResponse = { volumes: Volume[]; nextCursor: string | null }
export type IngressRule = { path: string; serviceId: string; port: number }
export type IngressListItem = { ingressId: string; host: string }
export type IngressListResponse = { ingresses: IngressListItem[]; nextCursor: string | null }
export type IngressDetail = {
  ingressId: string
  host: string
  rules: IngressRule[]
  pending_rules: IngressRule[] | null
  status: "active" | "applying" | "deleting"
}
export type LoginHistoryResponse = {
  history: { historyId: string; ip: string; userAgent: string; createdAt: string }[]
  nextCursor: string | null
}

export type BuildListItem = { buildId: string; status: string; imageRef: string; createdAt: string }
export type BuildListResponse = { builds: BuildListItem[]; nextCursor: string | null }
export type QuotaUsage = { used: number; limit: number }
/** GET /api/v1/quota の応答(docs/endpoints/quota.md)。 */
export type QuotaResponse = {
  maxReplicas: number
  maxVolumeSizeMB: number
  projects: QuotaUsage
  deploys: QuotaUsage
  volumes: QuotaUsage
  volumeTotalMB: QuotaUsage
  deploysBySpec: Record<string, QuotaUsage>
}
/** GET /auth/login-history の応答(docs/endpoints/auth.md)。 */
export type LoginEvent = { kind: "signup" | "login"; result: "success" | "failure"; reason: string | null; ip: string; userAgent: string; at: string }
export type LoginEventsResponse = { events: LoginEvent[] }

/** プロジェクトの namespace で起きた、k8s の Event(GET /api/v1/projects/:projectId/events)。 */
export type ProjectEvent = {
  eventId: string
  type: "Normal" | "Warning"
  reason: string
  message: string
  objectKind: string
  objectName: string
  deployId: string | null
  /** 同じ Event が、起きた回数(k8s がまとめる)。 */
  count: number
  firstSeenAt: string
  lastSeenAt: string
}
export type ProjectEventListResponse = { events: ProjectEvent[]; nextCursor: string | null }

/** ConfigMap のファイル 1 つ。encoding が text なら内容はそのままの文字列、base64 なら base64 の文字列。 */
export type ConfigMapFile = { key: string; encoding: "text" | "base64"; content: string }
/** 取得のレスポンスのファイル(size は、元のバイト数)。 */
export type ConfigMapFileView = ConfigMapFile & { size: number }
/** GET /api/v1/configmaps/:configMapId(docs/endpoints/configmaps.md)。 */
export type ConfigMapDetail = {
  configMapId: string
  projectId: string
  name: string
  currentRevision: number
  /** files が、どの版の中身か。 */
  revision: number
  files: ConfigMapFileView[]
  createdAt: string
  updatedAt: string
}
export type ConfigMapListItem = {
  configMapId: string
  name: string
  currentRevision: number
  fileCount: number
  /** 保存する大きさの合計(バイト)。 */
  totalBytes: number
  /** 使っている deploy の数(pending も含む)。 */
  usedBy: number
  updatedAt: string
}
export type ConfigMapListResponse = { configmaps: ConfigMapListItem[]; nextCursor: string | null }
export type ConfigMapRevisionItem = { revision: number; fileCount: number; totalBytes: number; createdAt: string }
export type ConfigMapUsage = { deployId: string; revision: number; key: string; path: string; pending: boolean }

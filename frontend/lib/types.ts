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
export type DeployDetail = DeployListItem & {
  projectId: string
  pods: PodInfo[]
  logPods: LogPodInfo[]
  config: DeployConfig
  volumes: { mounts: MountItem[]; pending_mounts: MountItem[] | null }
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
export type QuotaResponse = {
  maxReplicasPerDeploy: number
  maxVolumeSizeMB: number
  projects: QuotaUsage
  deploys: QuotaUsage
  volumes: QuotaUsage
  volumeTotalMB: QuotaUsage
  specs: Record<"small" | "medium" | "large", QuotaUsage>
}

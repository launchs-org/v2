export type User = {
  userId: string
  userName: string
  status: "active" | "banned"
  role: "user" | "admin"
  createdAt: string
  /** 一時パスワードのままか(true の間は、パスワードの変更を先に求める)。 */
  mustChangePassword?: boolean
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
  /** タグ(昇順)。古い API では無い。 */
  tags?: string[]
  createdAt: string
}

export type DeployListResponse = { deploys: DeployListItem[]; nextCursor: string | null }
export type PodInfo = { podId: string; status: string }
export type LogPodInfo = { podId: string; running: boolean; startedAt: string; endedAt: string | null }
export type ServicePort = { name: string; port: number; protocol: "tcp" | "udp" }
export type ServiceInfo = {
  serviceId: string
  tags?: string[]
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
  pending_image_ref?: string | null
}
/** deploy が、共有の ConfigMap の 1 ファイルを、1 つの path にマウントする指定(dev-docs/endpoints/deploys.md)。 */
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
  tags?: string[]
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
/** ingress の一覧の 1 件に、タグを足した形。 */
export type IngressTagged = { tags?: string[] }
export type IngressListResponse = { ingresses: IngressListItem[]; nextCursor: string | null }
export type IngressDetail = {
  ingressId: string
  tags?: string[]
  host: string
  rules: IngressRule[]
  pending_rules: IngressRule[] | null
  status: "active" | "applying" | "deleting"
  /** アクセスの集計(Traefik のプラグイン launchs-stats)を有効にしているか。 */
  accessStats?: boolean
}
/** カスタムドメインの所有を確かめるために、DNS に設定してもらう内容。TXT か CNAME の、どちらかでよい。 */
/** ingress に追加したホスト名 1 件(<利用者が選んだ名前>.<ベースドメイン>)。追加するとすぐに有効(verified)になる。 */
export type IngressDomain = { domainId: string; hostname: string; status: "pending" | "verified"; verifiedAt: string | null }
/** プロジェクトのタスク(worker が記録した Workflow の履歴)。 */
export type TaskStatus = "running" | "succeeded" | "failed" | "canceled" | "lost"
/** タスクの手順(Activity)1 つの進み具合。extra は、後始末など、定義に無い手順。 */
export type TaskStep = { activity: string; label: string; state: "done" | "running" | "failed" | "waiting"; attempt?: number; error?: string; extra?: boolean }
/** タスクの進み具合。total が 0 のときは、手順の総数が決まらない(動いた手順だけを出す)。steps は、詳細のときだけ。 */
export type TaskProgress = { done: number; total: number; current: string; steps?: TaskStep[] }
export type ProjectTask = {
  taskId: string
  projectId: string
  projectName: string
  progress?: TaskProgress
  kind: string
  resourceId: string
  status: TaskStatus
  error: string
  startedAt: string
  finishedAt: string | null
  durationMs: number
  workflowId: string
  children?: ProjectTask[]
}
export type ProjectTaskListResponse = { tasks: ProjectTask[]; nextCursor: string | null; counts: { running: number; failedRecent: number } }

/** アクセスログの 1 行(1 リクエスト)。 */
export type AccessLogEntry = { time: string; method: string; url: string; status: number; durationMs: number; clientIp: string; bytes: number }
export type AccessLogsResponse = { logs: AccessLogEntry[]; nextCursor: string | null }
export type LoginHistoryResponse = {
  history: { historyId: string; ip: string; userAgent: string; createdAt: string }[]
  nextCursor: string | null
}

export type BuildListItem = { buildId: string; status: string; imageRef: string; createdAt: string }
export type BuildListResponse = { builds: BuildListItem[]; nextCursor: string | null }
export type QuotaUsage = { used: number; limit: number }
/** GET /api/v1/quota の応答(dev-docs/endpoints/quota.md)。 */
export type QuotaResponse = {
  maxReplicas: number
  maxVolumeSizeMB: number
  projects: QuotaUsage
  deploys: QuotaUsage
  volumes: QuotaUsage
  volumeTotalMB: QuotaUsage
  deploysBySpec: Record<string, QuotaUsage>
}
/** GET /auth/login-history の応答(dev-docs/endpoints/auth.md)。 */
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
/** GET /api/v1/configmaps/:configMapId(dev-docs/endpoints/configmaps.md)。 */
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

/** テンプレートの変数の型。 */
export type TemplateVariableType = "string" | "integer" | "password" | "choice" | "boolean"
/** テンプレートの、入力欄 1 つの定義(GET /api/v1/templates)。値は、全て、文字列で扱う。 */
export type TemplateVariable = {
  name: string
  label: string
  type: TemplateVariableType
  /** 既定の値。自動生成の password は、常に null。 */
  default: string | null
  required: boolean
  /** password を、入力が無いとき、自動で作るか。 */
  generate: boolean
  options: { value: string; label: string }[]
  min: number | null
  max: number | null
  minLength: number | null
  maxLength: number | null
  pattern: string | null
}
/** テンプレートが、何を作るかの要約。 */
export type TemplateSummary = { deploys: number; volumes: number; ingress: boolean; files: number; configmaps: number }
export type Template = {
  id: string
  name: string
  description: string
  icon: string
  category: string
  /** 作成が終わったあとに出す案内(${変数名} を含みうる)。無ければ空。 */
  notes: string
  /** この応答の版(x.y.z)。一覧では、既定の版。 */
  version: string
  /** 選べる全ての版(新しい順)。 */
  versions: string[]
  /** テンプレートの README(詳細だけ)。 */
  readme?: string
  variables: TemplateVariable[]
  summary: TemplateSummary
}
export type TemplateListResponse = { templates: Template[] }
/** テンプレートの実行の、ステップ 1 つの進み具合。 */
export type TemplateApplyStep = {
  kind: "volume" | "configmap" | "deploy" | "service" | "ingress"
  name: string
  status: "waiting" | "creating" | "done" | "failed"
  resourceId: string
  error: string | null
}
/** GET /api/v1/projects/:projectId/template-applies/:applyId(dev-docs/endpoints/templates.md)。 */
export type TemplateApplyProgress = {
  applyId: string
  templateId: string
  /** 使ったテンプレートの版(古い記録は空)。 */
  templateVersion: string
  status: "running" | "succeeded" | "failed"
  values: Record<string, string>
  steps: TemplateApplyStep[]
  error: string | null
  createdAt: string
  updatedAt: string
}

/** メトリクスの 1 点(ある時刻の、範囲全体の合計)。測れなかった値は null。 */
export type MetricPoint = { at: string; cpuMillicores: number | null; memoryBytes: number | null; pods: number; readyPods: number; restarts: number }
export type MetricsResponse = { range: "1h" | "6h" | "24h"; points: MetricPoint[] }
/** プロジェクトのタグと、そのタグが付いた deploy の数。 */
export type TagListResponse = { tags: { tag: string; deployCount: number; volumeCount: number; serviceCount: number; ingressCount: number }[] }
/** タグの一斉削除の結果。 */
export type TagDeleteItem = { kind: "deploy" | "volume" | "service" | "ingress"; id: string; message: string }
export type TagDeleteResponse = { accepted: TagDeleteItem[]; failed: TagDeleteItem[] }

/** API トークン(機械が API を呼ぶための JWT)の情報。トークンの本体は、発行したときだけ返る。 */
export type ApiToken = { tokenId: string; name: string; scope: string[]; deploys: string[]; projects: string[]; expiresAt: string | null; revokedAt: string | null; lastUsedAt: string | null; createdAt: string }
export type ApiTokenListResponse = { tokens: ApiToken[] }
export type ApiTokenCreateResponse = ApiToken & { token: string }

/** イメージの更新履歴の 1 件(反映の結果は、applied か failed)。 */
export type ImageHistoryItem = { previousImageRef: string; imageRef: string; result: "applied" | "failed"; at: string }

/** イメージの更新履歴の応答(新しい順)。 */
export type ImageHistoryResponse = { history: ImageHistoryItem[] }

/** 招待の状態(有効、使用済み、期限切れ、取り消し済み)。 */
export type InvitationStatus = "active" | "used" | "expired" | "revoked"

/** 招待の 1 件(コードは含まない。コードは、発行のときだけ分かる)。 */
export type Invitation = {
  invitationId: string
  note: string
  status: InvitationStatus
  createdBy: string
  createdAt: string
  expiresAt: string
  usedBy: string | null
  usedAt: string | null
}

/** 招待の一覧の応答。 */
export type InvitationListResponse = { invitations: Invitation[]; nextCursor: string | null }

/** 招待を発行した応答(code は、このときだけ分かる)。 */
export type InvitationCreateResponse = { invitationId: string; code: string; expiresAt: string }

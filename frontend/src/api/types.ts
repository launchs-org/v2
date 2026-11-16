// API のレスポンスの型(docs/endpoints/ の仕様に合わせる)。

/** プロジェクトの状態。deleting は削除中、delete_failed は削除が止まっている(もう一度削除を呼べる)。 */
export type ProjectStatus = "active" | "deleting" | "delete_failed";

/** プロジェクト1件。 */
export interface Project {
  projectId: string;
  name: string;
  status: ProjectStatus;
}

/** プロジェクトの一覧の1ページ。nextCursor が null なら、最後のページ。 */
export interface ProjectPage {
  projects: Project[];
  nextCursor: string | null;
}

/** 上限のある項目1つの、上限と使用量。 */
export interface QuotaItem {
  limit: number;
  used: number;
}

/** 自分の上限と使用量(GET /api/v1/quota)。 */
export interface Quota {
  projects: QuotaItem;
  deploys: QuotaItem;
  deploysBySpec: Record<string, QuotaItem>;
  volumes: QuotaItem;
  volumeTotalMB: QuotaItem;
  maxVolumeSizeMB: number;
  maxReplicas: number;
}

/** 認証コンテナの、公開の設定(GET /auth/config)。 */
export interface AuthConfig {
  /** 人間であることの確認(Cloudflare Turnstile)。enabled が true のとき、ログインとサインアップに、トークンが要る。 */
  turnstile: { enabled: boolean; siteKey?: string };
}

/** ログインしているユーザー。 */
export interface CurrentUser {
  userId: string;
  userName: string;
}

/** deploy の状態。"停止"という状態は無く、replicas を 0 にすると scaled_zero になる。 */
export type DeployStatus = "pending" | "active" | "applying" | "failed" | "scaled_zero" | "deleting" | "delete_failed";

/** deploy の一覧の1件(GET /projects/:projectId/deploys)。 */
export interface DeploySummary {
  deployId: string;
  status: DeployStatus;
  spec: string;
  replicas: number;
  imageRef: string;
  createdAt: string;
}

/** deploy の一覧の1ページ。nextCursor が null なら、最後のページ。 */
export interface DeployPage {
  deploys: DeploySummary[];
  nextCursor: string | null;
}

/** 稼働中の Pod 1つ。 */
export interface DeployPod {
  podId: string;
  status: string;
}

/** ログのある Pod 1つ(終了した Pod も含む)。 */
export interface DeployLogPod {
  podId: string;
  running: boolean;
  startedAt: string;
  endedAt: string | null;
}

/** deploy の詳細(GET /deploy/:deployId)。この画面で使う項目だけを持つ(config などは、後の画面で足す)。 */
export interface DeployDetail {
  deployId: string;
  /** deploy が属するプロジェクトの ID。 */
  projectId: string;
  status: DeployStatus;
  spec: string;
  replicas: number;
  pods: DeployPod[];
  logPods: DeployLogPod[];
  imageRef: string;
  createdAt: string;
  /** 起動設定(環境変数・コマンド・引数)。pending_* は、適用前の変更(無ければ null)。 */
  config: DeployConfig;
  /** ボリュームのマウント。 */
  volumes: DeployMounts;
  /** サービス(ポートの公開)。作っていなければ null。 */
  service: DeployService | null;
}

/** ログの取得の結果。nextSince を、次の since に渡す。 */
export interface DeployLogChunk {
  logs: string;
  nextSince: number;
}

/** 起動設定。env・command・args は、常に3つセットで置き換える。command と args が null なら、イメージの既定を使う。 */
export interface DeployConfig {
  env: Record<string, string>;
  pending_env: Record<string, string> | null;
  command: string | null;
  pending_command: string | null;
  args: string[] | null;
  pending_args: string[] | null;
}

/** ボリュームのマウント1つ。 */
export interface Mount {
  volumeId: string;
  mountPath: string;
}

/** deploy のボリュームのマウント構成。 */
export interface DeployMounts {
  mounts: Mount[];
  pending_mounts: Mount[] | null;
}

/** サービスのポート1つ。 */
export interface ServicePort {
  name: string;
  port: number;
  protocol: "tcp" | "udp";
}

/** サービスの状態(払い出しの状態。deploy の状態とは別)。 */
export type ServiceStatus = "pending" | "active" | "failed" | "deleting";

/** サービス(deploy に1つ)。 */
export interface DeployService {
  serviceId: string;
  ports: ServicePort[];
  pending_ports: ServicePort[] | null;
  status: ServiceStatus;
  dnsName: string | null;
  clusterIP: string | null;
}

/** ingress の path ルール1つ。 */
export interface IngressRule {
  path: string;
  serviceId: string;
  port: number;
}

/** ingress の状態。 */
export type IngressStatus = "applying" | "active" | "failed" | "deleting";

/** ingress の一覧の1件。 */
export interface IngressSummary {
  ingressId: string;
  host: string;
}

/** ingress の一覧の1ページ。 */
export interface IngressPage {
  ingresses: IngressSummary[];
  nextCursor: string | null;
}

/** ingress の詳細。 */
export interface Ingress {
  ingressId: string;
  host: string;
  rules: IngressRule[];
  pending_rules: IngressRule[] | null;
  status: IngressStatus;
}

/** ボリュームの状態。 */
export type VolumeStatus = "applying" | "active" | "failed" | "deleting";

/** ボリュームの一覧の1件(詳細にある mountedBy は、一覧には無い)。 */
export interface Volume {
  volumeId: string;
  name: string;
  status: VolumeStatus;
  sizeMB: number;
  pending_sizeMB: number | null;
  /** マウントしている deploy の ID(詳細だけ)。 */
  mountedBy?: string[];
}

/** ボリュームの一覧の1ページ。 */
export interface VolumePage {
  volumes: Volume[];
  nextCursor: string | null;
}

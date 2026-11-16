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
  status: DeployStatus;
  spec: string;
  replicas: number;
  pods: DeployPod[];
  logPods: DeployLogPod[];
  imageRef: string;
  createdAt: string;
}

/** ログの取得の結果。nextSince を、次の since に渡す。 */
export interface DeployLogChunk {
  logs: string;
  nextSince: number;
}

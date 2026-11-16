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

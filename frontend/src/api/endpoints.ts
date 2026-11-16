// 画面から使う API の呼び出し。パスと型を、ここにまとめる。
import type { HttpClient } from "./http_client";
import type { AuthConfig, Project, ProjectPage, Quota } from "./types";

/**
 * 認証の公開の設定(人間であることの確認を使うか、サイトキーは何か)を取る。認証は要らない。
 *
 * @param http API の呼び出し道具。
 * @param signal 中断の合図。
 * @returns 公開の設定。
 */
export function getAuthConfig(http: HttpClient, signal?: AbortSignal): Promise<AuthConfig> {
  return http.request<AuthConfig>("GET", "/auth/config", { authenticated: false, ...(signal === undefined ? {} : { signal }) });
}

/**
 * プロジェクトの一覧の、1ページを取る。
 *
 * @param http API の呼び出し道具。
 * @param cursor 前のページの nextCursor(最初のページは undefined)。
 * @param signal 中断の合図。
 * @returns 一覧の1ページ。
 */
export function listProjects(http: HttpClient, cursor: string | undefined, signal?: AbortSignal): Promise<ProjectPage> {
  // cursor があれば、クエリに付ける(解釈せず、そのまま渡す)。
  const query = cursor === undefined ? "" : `?cursor=${encodeURIComponent(cursor)}`;
  return http.request<ProjectPage>("GET", `/api/v1/projects${query}`, signal === undefined ? {} : { signal });
}

/**
 * プロジェクトを1件取る。
 *
 * @param http API の呼び出し道具。
 * @param projectId プロジェクトの ID。
 * @param signal 中断の合図。
 * @returns プロジェクト。
 */
export function getProject(http: HttpClient, projectId: string, signal?: AbortSignal): Promise<Project> {
  // ID は、パスの一部なので、エンコードする。
  return http.request<Project>("GET", `/api/v1/projects/${encodeURIComponent(projectId)}`, signal === undefined ? {} : { signal });
}

/**
 * プロジェクトを作る。
 *
 * @param http API の呼び出し道具。
 * @param name プロジェクトの名前。
 * @returns 作ったプロジェクトの ID。
 */
export function createProject(http: HttpClient, name: string): Promise<{ projectId: string }> {
  return http.request<{ projectId: string }>("POST", "/api/v1/projects", { body: { name } });
}

/**
 * プロジェクトの名前を変える。
 *
 * @param http API の呼び出し道具。
 * @param projectId プロジェクトの ID。
 * @param name 新しい名前。
 */
export async function renameProject(http: HttpClient, projectId: string, name: string): Promise<void> {
  await http.request<unknown>("PATCH", `/api/v1/projects/${encodeURIComponent(projectId)}`, { body: { name } });
}

/**
 * プロジェクトの削除を、受け付けてもらう(実体の削除は、非同期に進む)。
 *
 * @param http API の呼び出し道具。
 * @param projectId プロジェクトの ID。
 */
export async function deleteProject(http: HttpClient, projectId: string): Promise<void> {
  await http.request<unknown>("DELETE", `/api/v1/projects/${encodeURIComponent(projectId)}`);
}

/**
 * 自分の上限と使用量を取る。
 *
 * @param http API の呼び出し道具。
 * @param signal 中断の合図。
 * @returns 上限と使用量。
 */
export function getQuota(http: HttpClient, signal?: AbortSignal): Promise<Quota> {
  return http.request<Quota>("GET", "/api/v1/quota", signal === undefined ? {} : { signal });
}

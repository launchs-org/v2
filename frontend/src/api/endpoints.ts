// 画面から使う API の呼び出し。パスと型を、ここにまとめる。
import type { HttpClient } from "./http_client";
import type { AuthConfig, DeployDetail, DeployLogChunk, DeployPage, Project, ProjectPage, Quota } from "./types";

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

/**
 * プロジェクトの deploy の一覧の、1ページを取る(新しい順)。
 *
 * @param http API の呼び出し道具。
 * @param projectId プロジェクトの ID。
 * @param cursor 前のページの nextCursor(最初のページは undefined)。
 * @param signal 中断の合図。
 * @returns 一覧の1ページ。
 */
export function listDeploys(http: HttpClient, projectId: string, cursor: string | undefined, signal?: AbortSignal): Promise<DeployPage> {
  // cursor があれば、クエリに付ける(解釈せず、そのまま渡す)。
  const query = cursor === undefined ? "" : `?cursor=${encodeURIComponent(cursor)}`;
  return http.request<DeployPage>("GET", `/api/v1/projects/${encodeURIComponent(projectId)}/deploys${query}`, signal === undefined ? {} : { signal });
}

/**
 * イメージから、deploy を作る(起動は、非同期に進む)。
 *
 * @param http API の呼び出し道具。
 * @param input プロジェクトの ID、イメージ、スペック。
 * @returns 作った deploy の ID。
 */
export function createDeploy(http: HttpClient, input: { projectId: string; imageRef: string; spec: string }): Promise<{ deployId: string }> {
  return http.request<{ deployId: string }>("POST", "/api/v1/deploy", { body: input });
}

/**
 * deploy の詳細を取る。
 *
 * @param http API の呼び出し道具。
 * @param deployId deploy の ID。
 * @param signal 中断の合図。
 * @returns 詳細。
 */
export function getDeploy(http: HttpClient, deployId: string, signal?: AbortSignal): Promise<DeployDetail> {
  return http.request<DeployDetail>("GET", `/api/v1/deploy/${encodeURIComponent(deployId)}`, signal === undefined ? {} : { signal });
}

/**
 * deploy のレプリカ数を変える(0 で止まる)。反映は、非同期に進む。
 *
 * @param http API の呼び出し道具。
 * @param deployId deploy の ID。
 * @param replicas 希望するレプリカ数。
 */
export async function scaleDeploy(http: HttpClient, deployId: string, replicas: number): Promise<void> {
  await http.request<unknown>("POST", `/api/v1/deploy/${encodeURIComponent(deployId)}/scale`, { body: { replicas } });
}

/**
 * deploy の削除を、受け付けてもらう(実体の削除は、非同期に進む)。
 *
 * @param http API の呼び出し道具。
 * @param deployId deploy の ID。
 */
export async function deleteDeploy(http: HttpClient, deployId: string): Promise<void> {
  await http.request<unknown>("DELETE", `/api/v1/deploy/${encodeURIComponent(deployId)}`);
}

/**
 * Pod 1つのログを、since の続きから取る。
 *
 * @param http API の呼び出し道具。
 * @param deployId deploy の ID。
 * @param podId Pod の ID。
 * @param since 前回の nextSince(最初は undefined で、先頭から)。
 * @param signal 中断の合図。
 * @returns ログの文字列と、次の since。
 */
export function getDeployLogs(http: HttpClient, deployId: string, podId: string, since: number | undefined, signal?: AbortSignal): Promise<DeployLogChunk> {
  // podId は必須。since があれば、続きから。
  const query = `?podId=${encodeURIComponent(podId)}${since === undefined ? "" : `&since=${since}`}`;
  return http.request<DeployLogChunk>("GET", `/api/v1/deploy/${encodeURIComponent(deployId)}/logs${query}`, signal === undefined ? {} : { signal });
}

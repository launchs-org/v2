// 画面から使う API の呼び出し。パスと型を、ここにまとめる。
import type { HttpClient } from "./http_client";
import type { AuthConfig, DeployDetail, DeployLogChunk, DeployPage, Ingress, IngressPage, IngressRule, Mount, Project, ProjectPage, Quota, ServicePort, Volume, VolumePage } from "./types";

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

/**
 * 本文の無い操作(適用・破棄・削除など)を、1回呼ぶ。
 *
 * @param http API の呼び出し道具。
 * @param method HTTP メソッド。
 * @param path サイトの絶対パス。
 * @param body 送る本文(無ければ undefined)。
 */
async function perform(http: HttpClient, method: string, path: string, body?: unknown): Promise<void> {
  // 本文があるときだけ、ボディに入れる。
  await http.request<unknown>(method, path, body === undefined ? {} : { body });
}

/**
 * cursor を、クエリ文字列にする(解釈せず、そのまま渡す)。
 *
 * @param cursor 前のページの nextCursor(最初のページは undefined)。
 * @returns 「?cursor=...」または空文字。
 */
function cursorQuery(cursor: string | undefined): string {
  return cursor === undefined ? "" : `?cursor=${encodeURIComponent(cursor)}`;
}

/** deploy のパス(ID をエンコードする)。 */
function deployPath(deployId: string): string {
  return `/api/v1/deploy/${encodeURIComponent(deployId)}`;
}

/** プロジェクトのパス(ID をエンコードする)。 */
function projectPath(projectId: string): string {
  return `/api/v1/projects/${encodeURIComponent(projectId)}`;
}

/**
 * 起動設定(env・command・args)を、全件で置き換えて、pending として保存する。
 *
 * @param http API の呼び出し道具。
 * @param deployId deploy の ID。
 * @param config 新しい起動設定。
 */
export function updateConfig(http: HttpClient, deployId: string, config: { env: Record<string, string>; command: string | null; args: string[] | null }): Promise<void> {
  return perform(http, "PUT", `${deployPath(deployId)}/config`, config);
}

/** 起動設定の pending を、適用する(コンテナが再起動する)。 */
export function applyConfig(http: HttpClient, deployId: string): Promise<void> {
  return perform(http, "POST", `${deployPath(deployId)}/config/apply`);
}

/** 起動設定の pending を、破棄する。 */
export function discardConfig(http: HttpClient, deployId: string): Promise<void> {
  return perform(http, "POST", `${deployPath(deployId)}/config/discard`);
}

/** ボリュームのマウントを、全件で置き換えて、pending として保存する。 */
export function updateMounts(http: HttpClient, deployId: string, mounts: Mount[]): Promise<void> {
  return perform(http, "PUT", `${deployPath(deployId)}/mounts`, { mounts });
}

/** マウントの pending を、適用する(コンテナが再起動する)。 */
export function applyMounts(http: HttpClient, deployId: string): Promise<void> {
  return perform(http, "POST", `${deployPath(deployId)}/mounts/apply`);
}

/** マウントの pending を、破棄する。 */
export function discardMounts(http: HttpClient, deployId: string): Promise<void> {
  return perform(http, "POST", `${deployPath(deployId)}/mounts/discard`);
}

/** サービスを作る(ポートを公開する)。 */
export function createService(http: HttpClient, deployId: string, ports: ServicePort[]): Promise<void> {
  return perform(http, "POST", `${deployPath(deployId)}/service`, { ports });
}

/** サービスのポートを、全件で置き換えて、pending として保存する。 */
export function updateServicePorts(http: HttpClient, deployId: string, ports: ServicePort[]): Promise<void> {
  return perform(http, "PATCH", `${deployPath(deployId)}/service`, { ports });
}

/** サービスのポートの pending を、適用する(Pod は再起動しない)。 */
export function applyServicePorts(http: HttpClient, deployId: string): Promise<void> {
  return perform(http, "POST", `${deployPath(deployId)}/service/apply`);
}

/** サービスのポートの pending を、破棄する。 */
export function discardServicePorts(http: HttpClient, deployId: string): Promise<void> {
  return perform(http, "POST", `${deployPath(deployId)}/service/discard`);
}

/** サービスを削除する(非同期に消える)。 */
export function deleteService(http: HttpClient, deployId: string): Promise<void> {
  return perform(http, "DELETE", `${deployPath(deployId)}/service`);
}

/**
 * deploy のサービスを取る(作っていなければ 404)。
 *
 * @param http API の呼び出し道具。
 * @param deployId deploy の ID。
 * @param signal 中断の合図。
 * @returns サービス。
 */
export function getService(http: HttpClient, deployId: string, signal?: AbortSignal): Promise<import("./types").DeployService> {
  return http.request("GET", `${deployPath(deployId)}/service`, signal === undefined ? {} : { signal });
}

/** ingress を作る(ホスト名は、自動で払い出される)。 */
export function createIngress(http: HttpClient, projectId: string): Promise<{ ingressId: string }> {
  return http.request<{ ingressId: string }>("POST", `${projectPath(projectId)}/ingresses`);
}

/** ingress の一覧の、1ページを取る(新しい順)。 */
export function listIngresses(http: HttpClient, projectId: string, cursor: string | undefined, signal?: AbortSignal): Promise<IngressPage> {
  return http.request<IngressPage>("GET", `${projectPath(projectId)}/ingresses${cursorQuery(cursor)}`, signal === undefined ? {} : { signal });
}

/** ingress を1件取る。 */
export function getIngress(http: HttpClient, projectId: string, ingressId: string, signal?: AbortSignal): Promise<Ingress> {
  return http.request<Ingress>("GET", `${projectPath(projectId)}/ingresses/${encodeURIComponent(ingressId)}`, signal === undefined ? {} : { signal });
}

/** ingress を削除する(非同期に消える)。 */
export function deleteIngress(http: HttpClient, projectId: string, ingressId: string): Promise<void> {
  return perform(http, "DELETE", `${projectPath(projectId)}/ingresses/${encodeURIComponent(ingressId)}`);
}

/** ingress の rules を、全件で置き換えて、pending として保存する。 */
export function updateIngressRules(http: HttpClient, projectId: string, ingressId: string, rules: IngressRule[]): Promise<void> {
  return perform(http, "PUT", `${projectPath(projectId)}/ingresses/${encodeURIComponent(ingressId)}`, { rules });
}

/** ingress の pending を、適用する。 */
export function applyIngressRules(http: HttpClient, projectId: string, ingressId: string): Promise<void> {
  return perform(http, "POST", `${projectPath(projectId)}/ingresses/${encodeURIComponent(ingressId)}/apply`);
}

/** ingress の pending を、破棄する。 */
export function discardIngressRules(http: HttpClient, projectId: string, ingressId: string): Promise<void> {
  return perform(http, "POST", `${projectPath(projectId)}/ingresses/${encodeURIComponent(ingressId)}/discard`);
}

/** volume を作る。 */
export function createVolume(http: HttpClient, projectId: string, input: { name: string; sizeMB: number }): Promise<Volume> {
  return http.request<Volume>("POST", `${projectPath(projectId)}/volumes`, { body: input });
}

/** volume の一覧の、1ページを取る(新しい順)。 */
export function listVolumes(http: HttpClient, projectId: string, cursor: string | undefined, signal?: AbortSignal): Promise<VolumePage> {
  return http.request<VolumePage>("GET", `${projectPath(projectId)}/volumes${cursorQuery(cursor)}`, signal === undefined ? {} : { signal });
}

/** volume を1件取る(マウントしている deploy も分かる)。 */
export function getVolume(http: HttpClient, projectId: string, volumeId: string, signal?: AbortSignal): Promise<Volume> {
  return http.request<Volume>("GET", `${projectPath(projectId)}/volumes/${encodeURIComponent(volumeId)}`, signal === undefined ? {} : { signal });
}

/** volume の名前を変える(すぐに反映される)。 */
export function renameVolume(http: HttpClient, projectId: string, volumeId: string, name: string): Promise<void> {
  return perform(http, "PATCH", `${projectPath(projectId)}/volumes/${encodeURIComponent(volumeId)}`, { name });
}

/** volume の拡張(大きさ)を、pending として保存する(縮小はできない)。 */
export function updateVolumeSize(http: HttpClient, projectId: string, volumeId: string, sizeMB: number): Promise<void> {
  return perform(http, "PATCH", `${projectPath(projectId)}/volumes/${encodeURIComponent(volumeId)}/size`, { sizeMB });
}

/** volume の拡張の pending を、適用する。 */
export function applyVolumeSize(http: HttpClient, projectId: string, volumeId: string): Promise<void> {
  return perform(http, "POST", `${projectPath(projectId)}/volumes/${encodeURIComponent(volumeId)}/apply`);
}

/** volume の拡張の pending を、破棄する。 */
export function discardVolumeSize(http: HttpClient, projectId: string, volumeId: string): Promise<void> {
  return perform(http, "POST", `${projectPath(projectId)}/volumes/${encodeURIComponent(volumeId)}/discard`);
}

/** volume を削除する(マウント中は 409。非同期に消える)。 */
export function deleteVolume(http: HttpClient, projectId: string, volumeId: string): Promise<void> {
  return perform(http, "DELETE", `${projectPath(projectId)}/volumes/${encodeURIComponent(volumeId)}`);
}

// プロジェクトの、deploy・ingress・volume の一覧を、全件取る(概要の図と、数のカード、一覧の画面が、同じものを使う)。
import { pollInterval } from "../lib/polling";
import { useQuery } from "@tanstack/react-query";
import { listDeploys, listIngresses, listVolumes } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { isDeployTransitioning } from "../lib/deploy";
import { fetchAllPages } from "../lib/fetch_all";

/**
 * プロジェクトの、deploy・ingress・volume の一覧(全件)を取る。状態が変わっていくものがあれば、定期的に確かめる。
 *
 * @param projectId プロジェクトの ID。
 * @returns 3つの一覧の問い合わせ。
 */
export function useProjectResources(projectId: string) {
  const session = useSession();
  const deploys = useQuery({
    queryKey: ["deploys", projectId, "all"],
    queryFn: ({ signal }) => fetchAllPages((cursor) => listDeploys(session.http, projectId, cursor, signal).then((page) => ({ items: page.deploys, next: page.nextCursor }))),
    refetchInterval: (query) => pollInterval(query.state.data?.some((deploy) => isDeployTransitioning(deploy.status)) === true),
  });
  const ingresses = useQuery({
    queryKey: ["ingresses", projectId, "all"],
    queryFn: ({ signal }) => fetchAllPages((cursor) => listIngresses(session.http, projectId, cursor, signal).then((page) => ({ items: page.ingresses, next: page.nextCursor }))),
  });
  const volumes = useQuery({
    queryKey: ["volumes", projectId, "all"],
    queryFn: ({ signal }) => fetchAllPages((cursor) => listVolumes(session.http, projectId, cursor, signal).then((page) => ({ items: page.volumes, next: page.nextCursor }))),
    refetchInterval: (query) => pollInterval(query.state.data?.some((volume) => volume.status === "applying" || volume.status === "deleting") === true),
  });
  return { deploys, ingresses, volumes };
}

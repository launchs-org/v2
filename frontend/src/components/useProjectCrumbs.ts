// ingress・volume の画面の、パンくず(プロジェクト → 自分)を作る。プロジェクトの名前は、遷移で渡されたものか、無ければ取りに行く。
import { useQuery } from "@tanstack/react-query";
import { useLocation } from "react-router-dom";
import { getProject } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import type { Crumb } from "./PageHeader";

/**
 * location.state から、プロジェクトの名前を読む。
 *
 * @param state 画面遷移で渡された値(何でも入りうる)。
 * @param projectId いま見ているプロジェクトの ID(違うプロジェクトの名前は使わない)。
 * @returns 名前。使えなければ null。
 */
function readProjectName(state: unknown, projectId: string): string | null {
  // オブジェクトで、ID が一致し、名前が文字列のときだけ使う。
  if (typeof state === "object" && state !== null) {
    const candidate = state as Record<string, unknown>;
    if (candidate.projectId === projectId && typeof candidate.projectName === "string") {
      return candidate.projectName;
    }
  }
  return null;
}

/**
 * パンくずの先頭(プロジェクトの一覧と、そのプロジェクト)を作る。
 *
 * @param projectId プロジェクトの ID。
 * @returns パンくず(名前が分かるまでは、一覧だけ)。
 */
export function useProjectCrumbs(projectId: string): Crumb[] {
  const session = useSession();
  const location = useLocation();
  const passedName = readProjectName(location.state, projectId);
  // 名前が渡されていないときだけ、プロジェクトを取る。
  const project = useQuery({
    queryKey: ["project", projectId],
    queryFn: ({ signal }) => getProject(session.http, projectId, signal),
    enabled: passedName === null,
  });
  const name = passedName ?? project.data?.name ?? null;
  // 名前が分かれば、そのプロジェクトへのリンクも付ける。
  return name === null ? [{ label: "プロジェクト", to: "/projects" }] : [{ label: "プロジェクト", to: "/projects" }, { label: name, to: `/projects/${projectId}` }];
}

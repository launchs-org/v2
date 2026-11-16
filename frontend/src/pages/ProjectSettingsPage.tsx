// プロジェクトの設定の画面(名前の変更、削除)。
import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { getProject } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ProjectSettingsPanel } from "../components/ProjectSettingsPanel";

/** プロジェクトの設定の画面。 */
export function ProjectSettingsPage() {
  const { projectId = "" } = useParams();
  const session = useSession();
  // 枠(ProjectLayout)が取ったものと、同じ問い合わせ(キャッシュを使う)。
  const project = useQuery({ queryKey: ["project", projectId], queryFn: ({ signal }) => getProject(session.http, projectId, signal) });
  return project.data === undefined ? null : <ProjectSettingsPanel project={project.data} />;
}

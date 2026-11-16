// プロジェクトの概要(リソースの数と、ingress → deploy → volume のつながりの図)。
import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Globe, HardDrive, Rocket } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { getProject } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ProjectCanvas } from "../components/ProjectCanvas";
import { useProjectResources } from "../components/useProjectResources";
import { Button } from "../components/ui/button";
import { formatMegabytes } from "../lib/format";

/** プロジェクトの概要の画面。 */
export function ProjectOverviewPage() {
  const { projectId = "" } = useParams();
  const session = useSession();
  const project = useQuery({ queryKey: ["project", projectId], queryFn: ({ signal }) => getProject(session.http, projectId, signal) });
  const { deploys, ingresses, volumes } = useProjectResources(projectId);
  const base = `/projects/${projectId}`;

  const data = project.data;
  if (data === undefined) {
    return null;
  }

  // 合計ボリューム容量 (MB)
  const totalVolumeMB = (volumes.data ?? []).reduce((acc, v) => acc + v.sizeMB, 0);

  return (
    <div className="flex flex-col gap-5">
      {/* ========================================================================= */}
      {/* Quick Status & Stats Ribbon */}
      {/* ========================================================================= */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {/* Deploys Stat */}
        <Link
          to={`${base}/deploys`}
          className="group relative overflow-hidden rounded-xl border border-border/80 bg-card/60 p-4 transition-all duration-200 hover:border-border hover:bg-card hover:shadow-sm"
        >
          <div className="flex items-center justify-between">
            <span className="flex size-8 items-center justify-center rounded-lg bg-indigo-500/10 text-indigo-400">
              <Rocket className="size-4" />
            </span>
            <ArrowUpRight className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
          <div className="mt-3">
            <div className="text-xl font-bold tracking-tight text-foreground">
              {deploys.data?.length ?? 0}
              <span className="ml-1 text-xs font-normal text-muted-foreground">サービス</span>
            </div>
            <p className="text-xs text-muted-foreground">デプロイ済みのコンテナ</p>
          </div>
        </Link>

        {/* Ingresses Stat */}
        <Link
          to={`${base}/ingresses`}
          className="group relative overflow-hidden rounded-xl border border-border/80 bg-card/60 p-4 transition-all duration-200 hover:border-border hover:bg-card hover:shadow-sm"
        >
          <div className="flex items-center justify-between">
            <span className="flex size-8 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-400">
              <Globe className="size-4" />
            </span>
            <ArrowUpRight className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
          <div className="mt-3">
            <div className="text-xl font-bold tracking-tight text-foreground">
              {ingresses.data?.length ?? 0}
              <span className="ml-1 text-xs font-normal text-muted-foreground">エンドポイント</span>
            </div>
            <p className="text-xs text-muted-foreground">外部公開ルート (Ingress)</p>
          </div>
        </Link>

        {/* Volumes Stat */}
        <Link
          to={`${base}/volumes`}
          className="group relative overflow-hidden rounded-xl border border-border/80 bg-card/60 p-4 transition-all duration-200 hover:border-border hover:bg-card hover:shadow-sm"
        >
          <div className="flex items-center justify-between">
            <span className="flex size-8 items-center justify-center rounded-lg bg-purple-500/10 text-purple-400">
              <HardDrive className="size-4" />
            </span>
            <ArrowUpRight className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
          <div className="mt-3">
            <div className="text-xl font-bold tracking-tight text-foreground">
              {volumes.data?.length ?? 0}
              <span className="ml-1 text-xs font-normal text-muted-foreground">
                ({formatMegabytes(totalVolumeMB)})
              </span>
            </div>
            <p className="text-xs text-muted-foreground">永続ボリュームストレージ</p>
          </div>
        </Link>
      </div>

      {/* ========================================================================= */}
      {/* Topology Canvas (Full Showcase) */}
      {/* ========================================================================= */}
      <div className="relative overflow-hidden rounded-xl border border-border bg-card shadow-sm">
        <div className="flex items-center justify-between border-b border-border px-5 py-3.5 bg-muted/20">
          <div className="flex items-center gap-2.5">
            <span className="flex size-2 rounded-full bg-indigo-500" />
            <div>
              <h2 className="text-sm font-semibold tracking-tight text-foreground">リソース構成図 (Topology Canvas)</h2>
              <p className="text-[11px] text-muted-foreground">
                Ingress &rarr; Deploy &rarr; Volume の接続関係。右端の丸ハンドルをドラッグして線を引くと接続を作成できます。
              </p>
            </div>
          </div>
          {data.status === "active" && (
            <div className="flex items-center gap-2">
              <Button asChild size="sm" variant="outline" className="h-8 gap-1 text-xs">
                <Link to={`${base}/ingresses/new`}>+ ingress</Link>
              </Button>
              <Button asChild size="sm" variant="outline" className="h-8 gap-1 text-xs">
                <Link to={`${base}/volumes/new`}>+ volume</Link>
              </Button>
            </div>
          )}
        </div>

        <div className="h-[30rem] min-h-[460px] overflow-hidden">
          <ProjectCanvas projectId={projectId} canEdit={data.status === "active"} />
        </div>
      </div>
    </div>
  );
}

// プロジェクトの中の全ての画面に共通の枠(プロジェクトの読み込み、404、削除中の知らせ)。
// Vercel / Linear スタイルの洗練されたヘッダーとタブバーを提供する。
import { pollInterval } from "../lib/polling";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Globe, Plus } from "lucide-react";
import { useState } from "react";
import { Link, NavLink, Outlet, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/api_error";
import { getProject } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ErrorMessage } from "../components/ErrorMessage";
import { useProjectResources } from "../components/useProjectResources";
import { Alert } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";
import { projectStatusLabel, projectStatusTone } from "../lib/project_name";
import { cn } from "../lib/utils";

/** プロジェクトの中の画面の、共通の枠。 */
export function ProjectLayout() {
  const { projectId = "" } = useParams();
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [copied, setCopied] = useState(false);

  // プロジェクトを取る。削除中の間は、確かめ直す。削除が終わると、404 になる。
  const project = useQuery({
    queryKey: ["project", projectId],
    queryFn: ({ signal }) => getProject(session.http, projectId, signal),
    refetchInterval: (query) => pollInterval(query.state.data?.status === "deleting"),
    retry: (failureCount, error) => !(error instanceof ApiError && error.status === 404) && failureCount < 2,
  });

  const { deploys, ingresses, volumes } = useProjectResources(projectId);

  // 代表となる公開ホスト(ingress の先頭)
  const primaryIngress = ingresses.data?.[0];

  function copyDomain(host: string) {
    void navigator.clipboard.writeText(`https://${host}`);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  // 見つからない(他のユーザーのもの、削除が終わったもの)なら、一覧へ戻る道を出す。
  if (project.error instanceof ApiError && project.error.status === 404) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-4 py-16 text-center">
          <p className="font-medium">プロジェクトが見つかりません(削除されたか、存在しません)。</p>
          <Button onClick={() => { void queryClient.invalidateQueries({ queryKey: ["projects"] }); navigate("/projects"); }}>プロジェクトの一覧へ</Button>
        </CardContent>
      </Card>
    );
  }
  if (project.isPending) {
    return (
      <div role="status" aria-label="読み込み中" className="flex flex-col gap-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    );
  }
  if (project.isError) {
    return <ErrorMessage error={project.error} />;
  }

  const data = project.data;
  const basePath = `/projects/${projectId}`;

  const navTabs = [
    { to: basePath, label: "構成図 (Canvas)", end: true },
    { to: `${basePath}/deploys`, label: "デプロイ", count: deploys.data?.length },
    { to: `${basePath}/services`, label: "サービス" },
    { to: `${basePath}/ingresses`, label: "公開 (Ingress)", count: ingresses.data?.length },
    { to: `${basePath}/volumes`, label: "ストレージ (Volume)", count: volumes.data?.length },
    { to: `${basePath}/settings`, label: "設定" },
  ];

  return (
    <div className="flex flex-col gap-6">
      {/* ========================================================================= */}
      {/* Modern Project Header */}
      {/* ========================================================================= */}
      <div className="flex flex-col gap-4 border-b border-border pb-4">
        {/* Breadcrumb & Quick Actions */}
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Link to="/projects" className="transition-colors hover:text-foreground">
              プロジェクト
            </Link>
            <span>/</span>
            <span className="font-medium text-foreground">{data.name}</span>
          </div>

          <div className="flex items-center gap-2">
            {data.status === "active" && (
              <Button asChild size="sm" className="gap-1.5 shadow-sm">
                <Link to={`${basePath}/deploys/new`}>
                  <Plus aria-hidden="true" className="size-3.5" />
                  <span>deploy を作る</span>
                </Link>
              </Button>
            )}
          </div>
        </div>

        {/* Title, Status, and URL Bar */}
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold tracking-tight text-foreground">{data.name}</h1>
            <div className="flex items-center gap-1.5">
              <span className="relative flex size-2">
                {data.status === "active" && (
                  <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                )}
                <span
                  className={cn(
                    "relative inline-flex size-2 rounded-full",
                    data.status === "active" && "bg-emerald-500",
                    data.status === "deleting" && "bg-amber-500",
                    data.status === "delete_failed" && "bg-red-500"
                  )}
                />
              </span>
              <Badge variant={projectStatusTone(data.status)} className="font-mono text-[10px]">
                {projectStatusLabel(data.status)}
              </Badge>
            </div>
          </div>

          {/* Primary Ingress Host pill (clickable copy) */}
          {primaryIngress && (
            <div
              role="button"
              tabIndex={0}
              onClick={() => copyDomain(primaryIngress.host)}
              onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") copyDomain(primaryIngress.host); }}
              className="group flex cursor-pointer items-center gap-2 rounded-md border border-border/80 bg-muted/40 px-3 py-1 font-mono text-xs text-muted-foreground transition-all hover:border-border hover:bg-muted/80 hover:text-foreground"
              title="クリックして URL をコピー"
            >
              <Globe className="size-3.5 text-muted-foreground transition-colors group-hover:text-emerald-500" />
              <span>{primaryIngress.host}</span>
              {copied ? (
                <Check className="size-3 text-emerald-500" />
              ) : (
                <Copy className="size-3 opacity-50 group-hover:opacity-100" />
              )}
            </div>
          )}
        </div>

        {/* Navigation Tabs */}
        <nav className="-mb-4 flex gap-2 overflow-x-auto pt-2" aria-label="プロジェクトのナビゲーション">
          {navTabs.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={tab.end}
              className={({ isActive }) =>
                cn(
                  "-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors outline-none",
                  isActive
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:border-border hover:text-foreground"
                )
              }
            >
              <span>{tab.label}</span>
              {tab.count !== undefined && (
                <span className="rounded-full bg-muted px-1.5 py-0.2 text-[10px] font-mono text-muted-foreground">
                  {tab.count}
                </span>
              )}
            </NavLink>
          ))}
        </nav>
      </div>

      {data.status === "deleting" && <Alert variant="info">プロジェクトを削除しています。終わると、一覧に戻ります…</Alert>}
      {data.status === "delete_failed" && <Alert>プロジェクトの削除に失敗して、止まっています。「設定」から、もう一度、削除を試せます。</Alert>}

      <Outlet />
    </div>
  );
}

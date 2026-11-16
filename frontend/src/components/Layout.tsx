// 画面の枠(上のバー: ロゴ、プロジェクトの使用量、ユーザー、ログアウト)。
import { useQuery } from "@tanstack/react-query";
import { FolderKanban, LogOut } from "lucide-react";
import { Link, Outlet } from "react-router-dom";
import { getQuota } from "../api/endpoints";
import { useSession, useSessionState } from "../auth/AuthProvider";
import { Brand } from "./Brand";
import { Button } from "./ui/button";

/** ログイン後の、全ての画面に共通の枠。 */
export function Layout() {
  const session = useSession();
  const state = useSessionState();
  // 上限と使用量(バーに、プロジェクトの数を出す)。取れなくても、枠は出す。
  const quota = useQuery({ queryKey: ["quota"], queryFn: ({ signal }) => getQuota(session.http, signal) });
  const userName = state.status === "authenticated" ? state.user.userName : "";
  const projects = quota.data?.projects;
  // 使用量の割合(上限が 0 のときは、満杯とみなす)。
  const ratio = projects === undefined ? 0 : projects.limit === 0 ? 1 : Math.min(projects.used / projects.limit, 1);
  return (
    <>
      <header className="sticky top-0 z-10 border-b bg-card/90 backdrop-blur">
        <div className="mx-auto flex h-16 max-w-5xl items-center justify-between gap-3 px-4">
          <Link to="/projects" className="rounded-md" aria-label="launchs(プロジェクトの一覧へ)">
            <Brand />
          </Link>
          <div className="flex items-center gap-2 sm:gap-3">
            {projects && (
              <div className="hidden items-center gap-2 rounded-full bg-muted px-3 py-1.5 text-xs text-muted-foreground sm:flex" title="使っているプロジェクトの数 / 上限">
                <FolderKanban aria-hidden="true" className="size-4" />
                <span>プロジェクト {projects.used}/{projects.limit}</span>
                <span aria-hidden="true" className="h-1.5 w-12 overflow-hidden rounded-full bg-border">
                  <span className={`block h-full rounded-full ${ratio >= 1 ? "bg-destructive" : "bg-primary"}`} style={{ width: `${Math.round(ratio * 100)}%` }} />
                </span>
              </div>
            )}
            {userName !== "" && (
              <div className="flex items-center gap-2" title={userName}>
                <span aria-hidden="true" className="grid size-9 place-items-center rounded-full bg-primary text-sm font-medium uppercase text-primary-foreground">{userName.slice(0, 1)}</span>
                <span className="hidden max-w-32 truncate text-sm sm:inline">{userName}</span>
              </div>
            )}
            <Button variant="ghost" size="icon" aria-label="ログアウト" title="ログアウト" onClick={() => void session.logout()}>
              <LogOut aria-hidden="true" />
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-3xl px-4 pb-16 pt-8">
        <Outlet />
      </main>
    </>
  );
}

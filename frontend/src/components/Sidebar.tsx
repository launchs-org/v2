// 左のサイドバー(ロゴ、ナビゲーション、プロジェクトの使用量、ユーザー)。
import { useQuery } from "@tanstack/react-query";
import { FolderKanban, Gauge, LogOut } from "lucide-react";
import { NavLink } from "react-router-dom";
import { getQuota } from "../api/endpoints";
import { useSession, useSessionState } from "../auth/AuthProvider";
import { cn } from "../lib/utils";
import { Brand } from "./Brand";
import { Button } from "./ui/button";
import { Progress } from "./ui/progress";

/** ナビゲーションの項目。画面を足すときは、ここに足す。 */
const NAV_ITEMS = [
  { to: "/projects", label: "プロジェクト", icon: FolderKanban },
  { to: "/usage", label: "使用量", icon: Gauge },
] as const;

/** サイドバー。画面の高さいっぱいに固定され、内容は、縦にスクロールできる。 */
export function Sidebar() {
  const session = useSession();
  const state = useSessionState();
  // 上限と使用量(下のカードに、プロジェクトの数を出す)。取れなくても、枠は出す。
  const quota = useQuery({ queryKey: ["quota"], queryFn: ({ signal }) => getQuota(session.http, signal) });
  const userName = state.status === "authenticated" ? state.user.userName : "";
  const projects = quota.data?.projects;
  return (
    <aside className="sticky top-0 hidden h-screen w-64 shrink-0 flex-col border-r bg-sidebar text-sidebar-foreground md:flex">
      <div className="flex h-14 items-center px-5">
        <Brand />
      </div>
      <nav aria-label="メインメニュー" className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-2">
        <p className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">管理</p>
        {NAV_ITEMS.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              cn("flex h-9 items-center gap-3 rounded-md px-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground", isActive && "bg-accent text-accent-foreground")
            }
          >
            <Icon aria-hidden="true" className="size-4 text-muted-foreground" />
            {label}
          </NavLink>
        ))}
      </nav>
      {projects && (
        <div className="mx-3 mb-3 rounded-lg border bg-card p-3 text-xs">
          <div className="mb-2 flex items-center justify-between text-muted-foreground">
            <span>プロジェクト {projects.used}/{projects.limit}</span>
          </div>
          <Progress value={projects.used} max={projects.limit} label="プロジェクトの使用量" />
        </div>
      )}
      <div className="flex items-center gap-2 border-t p-3">
        <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-xs font-medium uppercase text-primary-foreground">{userName.slice(0, 1)}</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium" title={userName}>{userName}</span>
        <Button variant="ghost" size="icon" aria-label="ログアウト" title="ログアウト" onClick={() => void session.logout()}>
          <LogOut aria-hidden="true" />
        </Button>
      </div>
    </aside>
  );
}

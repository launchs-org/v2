// 画面の枠(ヘッダ: 名前、使用量、ログアウト)。
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet } from "react-router-dom";
import { getQuota } from "../api/endpoints";
import { useSession, useSessionState } from "../auth/AuthProvider";

/** ログイン後の、全ての画面に共通の枠。 */
export function Layout() {
  const session = useSession();
  const state = useSessionState();
  // 上限と使用量(ヘッダに、プロジェクトの数を出す)。取れなくても、枠は出す。
  const quota = useQuery({ queryKey: ["quota"], queryFn: ({ signal }) => getQuota(session.http, signal) });
  return (
    <>
      <header className="app-header">
        <Link to="/projects" className="brand">launchs</Link>
        <div className="app-header__right">
          {quota.data && (
            <span className="muted" title="使っているプロジェクトの数 / 上限">
              プロジェクト {quota.data.projects.used}/{quota.data.projects.limit}
            </span>
          )}
          {state.status === "authenticated" && <span>{state.user.userName}</span>}
          <button type="button" className="button button--ghost" onClick={() => void session.logout()}>ログアウト</button>
        </div>
      </header>
      <main className="page">
        <Outlet />
      </main>
    </>
  );
}

// 画面の枠(左にサイドバー、右に、画面の内容)。パソコンの画面を前提に、内容は、幅いっぱいに広げる。
import { Link, Outlet } from "react-router-dom";
import { Brand } from "./Brand";
import { Sidebar } from "./Sidebar";

/** ログイン後の、全ての画面に共通の枠。 */
export function Layout() {
  return (
    <div className="flex min-h-screen">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        {/* 狭い画面(サイドバーが隠れる幅)のための、ロゴだけのバー。パソコンの画面が前提なので、狭い画面では、最低限の表示にとどめる。 */}
        <header className="flex h-14 items-center border-b px-4 md:hidden">
          <Link to="/projects" aria-label="launchs(プロジェクトの一覧へ)"><Brand /></Link>
        </header>
        <main className="flex-1 px-6 py-8 lg:px-10">
          <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}

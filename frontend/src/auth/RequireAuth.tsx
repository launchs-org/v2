// ログインしていないと見られない画面を守る。
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { Spinner } from "../components/ui/spinner";
import { useSessionState } from "./AuthProvider";

/**
 * ログインしていれば子を表示し、していなければログイン画面へ送る(ログイン後に、元の画面へ戻れるよう、場所を渡す)。
 *
 * @param props.children 守る画面。
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const state = useSessionState();
  const location = useLocation();
  // 確かめている最中は、何も出さない(ログイン画面が一瞬出るのを防ぐ)。
  if (state.status === "loading") {
    return <div className="grid min-h-screen place-items-center text-muted-foreground" role="status"><Spinner className="size-6" /><span className="sr-only">読み込み中…</span></div>;
  }
  if (state.status === "anonymous") {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  }
  return <>{children}</>;
}

// ログインしていないと見られない画面を守る。
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
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
    return <p className="muted" role="status">読み込み中…</p>;
  }
  if (state.status === "anonymous") {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  }
  return <>{children}</>;
}

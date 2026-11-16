// ログインの状態を、画面の部品に渡す(React の Context)。
import { createContext, useContext, useEffect, useSyncExternalStore, type ReactNode } from "react";
import type { AuthSession, SessionState } from "./auth_session";

const SessionContext = createContext<AuthSession | null>(null);

/**
 * 子の部品に、ログインの状態を渡す。起動時に、保存してある refresh token で、ログインの状態を戻す。
 *
 * @param props.session ログインの状態の管理。
 * @param props.children 子の部品。
 */
export function AuthProvider({ session, children }: { session: AuthSession; children: ReactNode }) {
  useEffect(() => {
    // 起動時に、ログインの状態を戻す。他のタブでの、ログアウトも、反映する。
    void session.restore();
    window.addEventListener("storage", session.handleStorageEvent);
    return () => window.removeEventListener("storage", session.handleStorageEvent);
  }, [session]);
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}

/**
 * ログインの状態の管理(AuthSession)を取り出す。
 *
 * @returns AuthSession。AuthProvider の外で呼ぶと、例外。
 */
export function useSession(): AuthSession {
  const session = useContext(SessionContext);
  if (session === null) {
    throw new Error("useSession は、AuthProvider の中で使ってください");
  }
  return session;
}

/**
 * いまのログインの状態を取り出す(変わると、再描画される)。
 *
 * @returns ログインの状態。
 */
export function useSessionState(): SessionState {
  const session = useSession();
  return useSyncExternalStore(session.subscribe, session.getSnapshot);
}

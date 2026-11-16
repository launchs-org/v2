// ログインとサインアップに共通の、フォーム。
import { useState, type FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { ErrorMessage } from "../components/ErrorMessage";
import { useSession, useSessionState } from "../auth/AuthProvider";

/** フォームの種類。 */
type Mode = "login" | "signup";

/**
 * ログインまたはサインアップの画面。ログイン済みなら、プロジェクトの一覧へ送る。
 *
 * @param props.mode login か signup。
 */
export function AuthForm({ mode }: { mode: Mode }) {
  const session = useSession();
  const state = useSessionState();
  const navigate = useNavigate();
  const location = useLocation();
  const [userName, setUserName] = useState("");
  const [password, setPassword] = useState("");
  const [invitationCode, setInvitationCode] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [submitting, setSubmitting] = useState(false);
  // ログイン後に戻る場所(保護された画面から送られてきたとき)。外部の URL に飛ばされないよう、サイト内のパスだけを受け付ける。
  const from = (location.state as { from?: unknown } | null)?.from;
  const destination = typeof from === "string" && from.startsWith("/") && !from.startsWith("//") ? from : "/projects";

  if (state.status === "authenticated") {
    return <Navigate to={destination} replace />;
  }

  /**
   * 送信されたとき: ログインまたはサインアップして、元の画面へ進む。失敗したら、エラーを出す。
   *
   * @param event フォームの送信イベント。
   */
  async function handleSubmit(event: FormEvent<HTMLFormElement>): Promise<void> {
    // ページの再読み込みを止める。
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      // 種類に応じて、呼ぶ。
      if (mode === "login") {
        await session.login(userName.trim(), password);
      } else {
        await session.signup(userName.trim(), password, invitationCode);
      }
      navigate(destination, { replace: true });
    } catch (cause) {
      setError(cause);
    } finally {
      setSubmitting(false);
    }
  }

  const isLogin = mode === "login";
  return (
    <main className="auth-page">
      <form className="card form" onSubmit={(event) => void handleSubmit(event)}>
        <h1>{isLogin ? "ログイン" : "アカウントを作る"}</h1>
        <label>
          ユーザー名
          <input value={userName} onChange={(event) => setUserName(event.target.value)} autoComplete="username" required maxLength={64} autoFocus />
        </label>
        <label>
          パスワード
          <input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete={isLogin ? "current-password" : "new-password"} required />
        </label>
        {!isLogin && <p className="muted">12文字以上。よくあるパスワードは使えません。</p>}
        {!isLogin && (
          <label>
            招待コード(招待制のときだけ)
            <input value={invitationCode} onChange={(event) => setInvitationCode(event.target.value)} autoComplete="off" />
          </label>
        )}
        <ErrorMessage error={error} />
        <button type="submit" className="button" disabled={submitting}>{submitting ? "送信中…" : isLogin ? "ログイン" : "作成してログイン"}</button>
        <p className="muted">
          {isLogin ? <>アカウントが無い方は <Link to="/signup">アカウントを作る</Link></> : <>アカウントがある方は <Link to="/login">ログイン</Link></>}
        </p>
      </form>
    </main>
  );
}

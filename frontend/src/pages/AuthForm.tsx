// ログインとサインアップに共通の、フォーム。
import { useState, type FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { ErrorMessage } from "../components/ErrorMessage";
import { Brand } from "../components/Brand";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Spinner } from "../components/ui/spinner";
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
    <main className="grid min-h-screen place-items-center px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex justify-center">
          <Brand large />
        </div>
        <Card>
          <CardContent className="p-8">
            <form className="flex flex-col gap-5" onSubmit={(event) => void handleSubmit(event)}>
              <div className="text-center">
                <h1 className="text-2xl font-normal">{isLogin ? "ログイン" : "アカウントを作る"}</h1>
                <p className="mt-1 text-sm text-muted-foreground">{isLogin ? "launchs を使うには、ログインしてください" : "ユーザー名とパスワードだけで、作れます"}</p>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="userName">ユーザー名</Label>
                <Input id="userName" value={userName} onChange={(event) => setUserName(event.target.value)} autoComplete="username" required maxLength={64} autoFocus />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="password">パスワード</Label>
                <Input id="password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete={isLogin ? "current-password" : "new-password"} required />
                {!isLogin && <p className="text-xs text-muted-foreground">12文字以上。よくあるパスワードは使えません。</p>}
              </div>
              {!isLogin && (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="invitationCode">招待コード(招待制のときだけ)</Label>
                  <Input id="invitationCode" value={invitationCode} onChange={(event) => setInvitationCode(event.target.value)} autoComplete="off" />
                </div>
              )}
              <ErrorMessage error={error} />
              <div className="flex items-center justify-between gap-3 pt-1">
                <Button asChild variant="link" className="px-0">
                  <Link to={isLogin ? "/signup" : "/login"}>{isLogin ? "アカウントを作る" : "ログインへ"}</Link>
                </Button>
                <Button type="submit" disabled={submitting}>
                  {submitting && <Spinner />}
                  {submitting ? "送信中…" : isLogin ? "ログイン" : "作成してログイン"}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      </div>
    </main>
  );
}

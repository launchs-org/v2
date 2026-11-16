// 画面の組み立て(ルーティング)。
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { ApiError } from "./api/api_error";
import type { AuthSession } from "./auth/auth_session";
import { AuthProvider } from "./auth/AuthProvider";
import { RequireAuth } from "./auth/RequireAuth";
import { Layout } from "./components/Layout";
import { AuthForm } from "./pages/AuthForm";
import { NotFoundPage } from "./pages/NotFoundPage";
import { ProjectPage } from "./pages/ProjectPage";
import { ProjectsPage } from "./pages/ProjectsPage";
import { UsagePage } from "./pages/UsagePage";

/**
 * 画面の全体を作る。
 *
 * @param props.session ログインの状態の管理。
 * @param props.queryClient サーバーの状態の入れ物(テストで差し替える)。
 */
export function App({ session, queryClient = createQueryClient() }: { session: AuthSession; queryClient?: QueryClient }) {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider session={session}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<AuthForm mode="login" />} />
            <Route path="/signup" element={<AuthForm mode="signup" />} />
            <Route element={<RequireAuth><Layout /></RequireAuth>}>
              <Route path="/projects" element={<ProjectsPage />} />
              <Route path="/projects/:projectId" element={<ProjectPage />} />
              <Route path="/usage" element={<UsagePage />} />
            </Route>
            <Route path="/" element={<Navigate to="/projects" replace />} />
            <Route path="*" element={<NotFoundPage />} />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
}

/**
 * サーバーの状態の入れ物を作る。4xx(認証・入力・存在しない)は、やり直しても直らないので、再試行しない。
 *
 * @returns QueryClient。
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (failureCount, error) => !(error instanceof ApiError && error.status >= 400 && error.status < 500) && failureCount < 2,
        refetchOnWindowFocus: false,
      },
    },
  });
}

// 画面のテスト用: 偽のサーバーで、アプリ全体を描画する。
import { QueryClient } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { App } from "../App";
import { AuthSession } from "../auth/auth_session";
import { REFRESH_TOKEN_KEY, TokenStore } from "../auth/token_store";
import { createFakeFetch, createMemoryStorage, errorResponse, jsonResponse, type Handler, type RecordedRequest } from "./fake_server";

/** 画面のテストの、偽のサーバーの、既定の応答を作るときの設定。 */
export interface ServerState {
  projects: { projectId: string; name: string; status: string }[];
  /** ページごとの、nextCursor(指定しなければ、1ページだけ)。 */
  pageSize?: number;
  quota?: { projectsUsed: number; projectsLimit: number };
  /** 指定すると、人間であることの確認(Turnstile)が有効になる(GET /auth/config が、サイトキーを返す)。 */
  turnstileSiteKey?: string;
}

/**
 * 認証と、プロジェクトの API を持つ、偽のサーバーを作る。
 *
 * @param state サーバーの状態(テストの中で、書き換えられる)。
 * @param overrides 「メソッド パス」ごとの、上書き。
 * @returns 応答を決める関数。
 */
export function appServer(state: ServerState, overrides: Record<string, Handler> = {}): Handler {
  return (request: RecordedRequest) => {
    const key = `${request.method} ${request.path.split("?")[0]}`;
    const override = overrides[key];
    if (override !== undefined) {
      return override(request);
    }
    if (key === "GET /auth/config") {
      return jsonResponse(200, { turnstile: state.turnstileSiteKey === undefined ? { enabled: false } : { enabled: true, siteKey: state.turnstileSiteKey } });
    }
    if (key === "POST /auth/login" || key === "POST /auth/signup") {
      return jsonResponse(200, { token: "refresh-1" });
    }
    if (key === "POST /auth/refresh") {
      return jsonResponse(200, { token: "access-1" });
    }
    if (key === "GET /auth/me") {
      return jsonResponse(200, { userId: "userid-1", userName: "alice" });
    }
    if (key === "POST /auth/logout") {
      return jsonResponse(200, {});
    }
    if (key === "GET /api/v1/quota") {
      return jsonResponse(200, { projects: { limit: state.quota?.projectsLimit ?? 10, used: state.quota?.projectsUsed ?? state.projects.length }, deploys: { limit: 20, used: 0 }, deploysBySpec: {}, volumes: { limit: 10, used: 0 }, volumeTotalMB: { limit: 20480, used: 0 }, maxVolumeSizeMB: 5120, maxReplicas: 5 });
    }
    if (key === "GET /api/v1/projects") {
      // cursor は、「何件目から」を表す文字列にする(クライアントは、解釈しない)。
      const cursor = new URL(request.path, "http://x").searchParams.get("cursor");
      const start = cursor === null ? 0 : Number(cursor);
      const size = state.pageSize ?? 100;
      const next = start + size < state.projects.length ? String(start + size) : null;
      return jsonResponse(200, { projects: state.projects.slice(start, start + size), nextCursor: next });
    }
    if (key === "POST /api/v1/projects") {
      const name = (request.body as { name: string }).name;
      state.projects.unshift({ projectId: `project-${name}`, name, status: "active" });
      return jsonResponse(200, { projectId: `project-${name}` });
    }
    // deploy の一覧は、既定では空(deploy の画面のテストは、上書きする)。
    if (/^GET \/api\/v1\/projects\/[^/]+\/deploys$/.test(key)) {
      return jsonResponse(200, { deploys: [], nextCursor: null });
    }
    const single = /^(GET|PATCH|DELETE) \/api\/v1\/projects\/([^/]+)$/.exec(key);
    if (single !== null) {
      const project = state.projects.find((candidate) => candidate.projectId === decodeURIComponent(single[2] ?? ""));
      if (project === undefined) {
        return errorResponse(404, "not_found", "プロジェクトが見つかりません");
      }
      if (single[1] === "PATCH") {
        project.name = (request.body as { name: string }).name;
        return jsonResponse(200, {});
      }
      if (single[1] === "DELETE") {
        project.status = "deleting";
        return jsonResponse(200, {});
      }
      return jsonResponse(200, project);
    }
    return errorResponse(404, "not_found", `想定外のリクエスト: ${key}`);
  };
}

/**
 * アプリを描画する。
 *
 * @param handler 偽のサーバー。
 * @param options loggedIn が true なら、refresh token が保存済みの状態から始める。path は、最初に開く URL。
 * @returns 記録されたリクエストと、ログインの状態の管理。
 */
export function renderApp(handler: Handler, options: { loggedIn?: boolean; path?: string } = {}) {
  window.history.pushState({}, "", options.path ?? "/");
  const storage = createMemoryStorage(options.loggedIn ? { [REFRESH_TOKEN_KEY]: "refresh-1" } : {});
  const { fetchImpl, requests } = createFakeFetch(handler);
  const session = new AuthSession(new TokenStore(storage), fetchImpl);
  // テストでは、失敗の再試行を待たない。
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<App session={session} queryClient={queryClient} />);
  return { requests, session, storage };
}

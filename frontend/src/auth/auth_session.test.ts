import { describe, expect, it } from "vitest";
import { AuthSession } from "./auth_session";
import { REFRESH_TOKEN_KEY, TokenStore } from "./token_store";
import { createFakeFetch, createMemoryStorage, errorResponse, jsonResponse, type Handler } from "../test/fake_server";

/**
 * 認証のコンテナの代わりの、偽のサーバーの応答。
 *
 * @param overrides パスごとに、応答を上書きする。
 * @returns 応答を決める関数。
 */
function authServer(overrides: Record<string, Handler> = {}): Handler {
  return (request) => {
    const override = overrides[`${request.method} ${request.path}`];
    if (override !== undefined) {
      return override(request);
    }
    switch (`${request.method} ${request.path}`) {
      case "POST /auth/login":
      case "POST /auth/signup":
        return jsonResponse(200, { token: "refresh-1" });
      case "POST /auth/refresh":
        return jsonResponse(200, { token: "access-1" });
      case "GET /auth/me":
        return jsonResponse(200, { userId: "userid-1", userName: "alice" });
      case "POST /auth/logout":
        return jsonResponse(200, {});
      default:
        return errorResponse(404, "not_found", "not found");
    }
  };
}

/**
 * セッションと、記録を作る。
 *
 * @param handler 偽のサーバー。
 * @param stored 最初に保存してある refresh token。
 * @returns セッション、保存先、記録。
 */
function setup(handler: Handler, stored?: string) {
  const storage = createMemoryStorage(stored === undefined ? {} : { [REFRESH_TOKEN_KEY]: stored });
  const { fetchImpl, requests } = createFakeFetch(handler);
  return { session: new AuthSession(new TokenStore(storage), fetchImpl), storage, requests };
}

describe("AuthSession", () => {
  it("login: refresh token を保存し、access token を取って、ユーザーを読み込む", async () => {
    const { session, storage, requests } = setup(authServer());
    await session.login("alice", "correct horse battery");
    expect(session.getSnapshot()).toEqual({ status: "authenticated", user: { userId: "userid-1", userName: "alice" } });
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBe("refresh-1");
    expect(session.getAccessToken()).toBe("access-1");
    expect(requests.map((request) => `${request.method} ${request.path}`)).toEqual(["POST /auth/login", "POST /auth/refresh", "GET /auth/me"]);
    // refresh は、refresh token を Authorization に入れ、me は、access token を入れる。
    expect(requests[1]?.headers["Authorization"]).toBe("Bearer refresh-1");
    expect(requests[2]?.headers["Authorization"]).toBe("Bearer access-1");
  });

  it("login が失敗したら、ログアウトの状態のままで、エラーを投げる(保存もしない)", async () => {
    const { session, storage } = setup(authServer({ "POST /auth/login": () => errorResponse(401, "unauthorized", "userName または password が正しくありません") }));
    await expect(session.login("alice", "wrong")).rejects.toMatchObject({ status: 401 });
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
    expect(session.getSnapshot().status).not.toBe("authenticated");
  });

  it("signup: 招待コードは、あるときだけ送る(前後の空白は除く)", async () => {
    const withCode = setup(authServer());
    await withCode.session.signup("alice", "correct horse battery", "  code-1  ");
    expect(withCode.requests[0]?.body).toEqual({ userName: "alice", password: "correct horse battery", invitationCode: "code-1" });
    const withoutCode = setup(authServer());
    await withoutCode.session.signup("alice", "correct horse battery", "   ");
    expect(withoutCode.requests[0]?.body).toEqual({ userName: "alice", password: "correct horse battery" });
  });

  it("login の途中(me)で失敗したら、保存した refresh token も消して、ログアウトに戻す", async () => {
    const { session, storage } = setup(authServer({ "GET /auth/me": () => errorResponse(500, "internal_error", "boom") }));
    await expect(session.login("alice", "pw")).rejects.toMatchObject({ status: 500 });
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
    expect(session.getSnapshot()).toEqual({ status: "anonymous" });
  });

  it("restore: refresh token が無ければ、サーバーに問い合わせず、ログアウトの状態にする", async () => {
    const { session, requests } = setup(authServer());
    await session.restore();
    expect(session.getSnapshot()).toEqual({ status: "anonymous" });
    expect(requests).toHaveLength(0);
  });

  it("restore: 保存してある refresh token で、ログインの状態を戻す", async () => {
    const { session } = setup(authServer(), "refresh-stored");
    expect(session.getSnapshot()).toEqual({ status: "loading" });
    await session.restore();
    expect(session.getSnapshot().status).toBe("authenticated");
  });

  it("restore: refresh token が無効(401)なら、消して、ログアウトの状態にする", async () => {
    const { session, storage } = setup(authServer({ "POST /auth/refresh": () => errorResponse(401, "unauthorized", "トークンが無効です") }), "refresh-revoked");
    await session.restore();
    expect(session.getSnapshot()).toEqual({ status: "anonymous" });
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
  });

  it("restore: 接続の失敗のときは、refresh token を残す(リロードで、やり直せる)", async () => {
    const storage = createMemoryStorage({ [REFRESH_TOKEN_KEY]: "refresh-stored" });
    const failing: typeof fetch = () => Promise.reject(new TypeError("Failed to fetch"));
    const session = new AuthSession(new TokenStore(storage), failing);
    await session.restore();
    expect(session.getSnapshot()).toEqual({ status: "anonymous" });
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBe("refresh-stored");
  });

  it("access token の期限切れ(401)では、更新して、1回だけやり直す", async () => {
    let quotaCalls = 0;
    const { session, requests } = setup(authServer({ "GET /api/v1/quota": () => (++quotaCalls === 1 ? errorResponse(401, "unauthorized", "認証が必要です") : jsonResponse(200, { ok: true })) }));
    await session.login("alice", "pw");
    expect(await session.http.request("GET", "/api/v1/quota")).toEqual({ ok: true });
    expect(requests.filter((request) => request.path === "/auth/refresh")).toHaveLength(2);
  });

  it("同時に何回 401 になっても、access token の更新は、1回だけ", async () => {
    let quotaCalls = 0;
    const { session, requests } = setup(authServer({ "GET /api/v1/quota": () => (++quotaCalls <= 3 ? errorResponse(401, "unauthorized", "認証が必要です") : jsonResponse(200, { ok: true })) }));
    await session.login("alice", "pw");
    const refreshesBefore = requests.filter((request) => request.path === "/auth/refresh").length;
    await Promise.all([session.http.request("GET", "/api/v1/quota"), session.http.request("GET", "/api/v1/quota"), session.http.request("GET", "/api/v1/quota")]);
    expect(requests.filter((request) => request.path === "/auth/refresh").length - refreshesBefore).toBe(1);
  });

  it("更新で refresh token が無効(401)と分かったら、ログアウトの状態になる", async () => {
    let refreshCalls = 0;
    const { session, storage } = setup(authServer({
      "POST /auth/refresh": () => (++refreshCalls === 1 ? jsonResponse(200, { token: "access-1" }) : errorResponse(401, "unauthorized", "トークンが無効です")),
      "GET /api/v1/quota": () => errorResponse(401, "unauthorized", "認証が必要です"),
    }));
    await session.login("alice", "pw");
    await expect(session.http.request("GET", "/api/v1/quota")).rejects.toMatchObject({ status: 401 });
    expect(session.getSnapshot()).toEqual({ status: "anonymous" });
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
  });

  it("logout: 先にこの端末をログアウトにして、サーバーの refresh token も無効にする。サーバーが失敗しても、ログアウトのまま", async () => {
    const { session, storage, requests } = setup(authServer({ "POST /auth/logout": () => errorResponse(500, "internal_error", "boom") }));
    await session.login("alice", "pw");
    await session.logout();
    expect(session.getSnapshot()).toEqual({ status: "anonymous" });
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
    expect(session.getAccessToken()).toBeNull();
    expect(requests.at(-1)).toMatchObject({ method: "POST", path: "/auth/logout" });
    expect(requests.at(-1)?.headers["Authorization"]).toBe("Bearer refresh-1");
  });

  it("他のタブでログアウトされたら(保存が消えたら)、このタブもログアウトにする", async () => {
    const { session, storage } = setup(authServer());
    await session.login("alice", "pw");
    storage.removeItem(REFRESH_TOKEN_KEY);
    session.handleStorageEvent(new StorageEvent("storage", { key: REFRESH_TOKEN_KEY }));
    expect(session.getSnapshot()).toEqual({ status: "anonymous" });
  });

  it("状態が変わると、受け取り手に知らせ、解除すると知らせない", async () => {
    const { session } = setup(authServer());
    let notified = 0;
    const unsubscribe = session.subscribe(() => notified++);
    await session.login("alice", "pw");
    expect(notified).toBeGreaterThan(0);
    const before = notified;
    unsubscribe();
    await session.logout();
    expect(notified).toBe(before);
  });
});

describe("TokenStore", () => {
  it("保存先が例外を投げても、例外にせず、無いものとして扱う", () => {
    const broken = {
      getItem: () => { throw new Error("denied"); },
      setItem: () => { throw new Error("denied"); },
      removeItem: () => { throw new Error("denied"); },
    };
    const store = new TokenStore(broken);
    expect(store.read()).toBeNull();
    expect(() => store.write("x")).not.toThrow();
    expect(() => store.clear()).not.toThrow();
    expect(new TokenStore(null).read()).toBeNull();
  });
});

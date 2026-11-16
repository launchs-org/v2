import { describe, expect, it, vi } from "vitest";
import { ApiError, describeError } from "./api_error";
import { HttpClient, type TokenSource } from "./http_client";
import { createFakeFetch, errorResponse, jsonResponse } from "../test/fake_server";

/**
 * テスト用のトークンの取得元を作る。
 *
 * @param accessToken いまの access token。
 * @param refreshed 更新したときに返す値(null なら、更新できない)。
 * @returns 取得元と、更新が呼ばれた回数を数える関数。
 */
function tokenSource(accessToken: string | null, refreshed: string | null): { source: TokenSource; refreshCalls: () => number } {
  const refresh = vi.fn(async () => refreshed);
  return { source: { getAccessToken: () => accessToken, refreshAccessToken: refresh }, refreshCalls: () => refresh.mock.calls.length };
}

describe("HttpClient", () => {
  it("access token を Authorization に付け、JSON のボディを送って、JSON を返す", async () => {
    const { fetchImpl, requests } = createFakeFetch(() => jsonResponse(200, { projectId: "p1" }));
    const { source } = tokenSource("access-1", null);
    const result = await new HttpClient(source, fetchImpl).request<{ projectId: string }>("POST", "/api/v1/projects", { body: { name: "app" } });
    expect(result).toEqual({ projectId: "p1" });
    expect(requests[0]).toMatchObject({ method: "POST", path: "/api/v1/projects", body: { name: "app" } });
    expect(requests[0]?.headers["Authorization"]).toBe("Bearer access-1");
    expect(requests[0]?.headers["Content-Type"]).toBe("application/json");
  });

  it("authenticated: false のときは、Authorization を付けず、401 でも更新しない", async () => {
    const { fetchImpl, requests } = createFakeFetch(() => errorResponse(401, "unauthorized", "userName または password が正しくありません"));
    const { source, refreshCalls } = tokenSource("access-1", "access-2");
    await expect(new HttpClient(source, fetchImpl).request("POST", "/auth/login", { body: {}, authenticated: false })).rejects.toMatchObject({ status: 401, code: "unauthorized" });
    expect(requests).toHaveLength(1);
    expect(requests[0]?.headers["Authorization"]).toBeUndefined();
    expect(refreshCalls()).toBe(0);
  });

  it("401 のときは、access token を更新して、1回だけやり直す", async () => {
    let calls = 0;
    const { fetchImpl, requests } = createFakeFetch(() => (++calls === 1 ? errorResponse(401, "unauthorized", "認証が必要です") : jsonResponse(200, { ok: true })));
    const { source, refreshCalls } = tokenSource("expired", "fresh");
    const result = await new HttpClient(source, fetchImpl).request("GET", "/api/v1/quota");
    expect(result).toEqual({ ok: true });
    expect(refreshCalls()).toBe(1);
    expect(requests.map((request) => request.headers["Authorization"])).toEqual(["Bearer expired", "Bearer fresh"]);
  });

  it("更新できないとき(null)は、元の 401 を、エラーにする。やり直しもしない", async () => {
    const { fetchImpl, requests } = createFakeFetch(() => errorResponse(401, "unauthorized", "認証が必要です"));
    const { source } = tokenSource("expired", null);
    await expect(new HttpClient(source, fetchImpl).request("GET", "/api/v1/quota")).rejects.toMatchObject({ status: 401 });
    expect(requests).toHaveLength(1);
  });

  it("やり直しても 401 なら、さらに更新はせず、エラーにする(無限に繰り返さない)", async () => {
    const { fetchImpl, requests } = createFakeFetch(() => errorResponse(401, "unauthorized", "認証が必要です"));
    const { source, refreshCalls } = tokenSource("expired", "still-bad");
    await expect(new HttpClient(source, fetchImpl).request("GET", "/api/v1/quota")).rejects.toMatchObject({ status: 401 });
    expect(requests).toHaveLength(2);
    expect(refreshCalls()).toBe(1);
  });

  it("エラーのボディ(code と message)と、Retry-After を、ApiError にする", async () => {
    const { fetchImpl } = createFakeFetch(() => errorResponse(429, "too_many_requests", "リクエストが多すぎます", { "Retry-After": "30" }));
    const error = await new HttpClient(tokenSource(null, null).source, fetchImpl).request("POST", "/auth/login", { authenticated: false }).catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 429, code: "too_many_requests", retryAfterSeconds: 30 });
    expect(describeError(error)).toContain("30 秒後");
  });

  it("形が違うエラー(プロキシの HTML など)は、ステータスから、汎用のメッセージにする", async () => {
    const { fetchImpl } = createFakeFetch(() => new Response("<html>Bad Gateway</html>", { status: 502 }));
    const error = await new HttpClient(tokenSource(null, null).source, fetchImpl).request("GET", "/api/v1/quota", { authenticated: false }).catch((cause: unknown) => cause);
    expect(error).toMatchObject({ status: 502, code: "internal_error" });
  });

  it("接続できないときは、network_error にする", async () => {
    const failing: typeof fetch = () => Promise.reject(new TypeError("Failed to fetch"));
    const error = await new HttpClient(tokenSource(null, null).source, failing).request("GET", "/api/v1/quota", { authenticated: false }).catch((cause: unknown) => cause);
    expect(error).toMatchObject({ status: 0, code: "network_error" });
    expect(describeError(error)).toContain("接続できません");
  });

  it("ボディが空の成功は、空のオブジェクトにする", async () => {
    const { fetchImpl } = createFakeFetch(() => new Response("", { status: 200 }));
    expect(await new HttpClient(tokenSource(null, null).source, fetchImpl).request("GET", "/x", { authenticated: false })).toEqual({});
  });

  it("想定外のエラーは、詳細を見せない", () => {
    expect(describeError(new Error("secret internals"))).toBe("予期しないエラーが起きました。");
  });
});

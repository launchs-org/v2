// テスト用の、偽のサーバー(fetch の代わり)。呼び出しを記録し、決めた応答を返す。

/** 偽のサーバーが受けた、1回のリクエスト。 */
export interface RecordedRequest {
  method: string;
  path: string;
  headers: Record<string, string>;
  body: unknown;
}

/** 応答を決める関数。リクエストを見て、Response を返す。 */
export type Handler = (request: RecordedRequest) => Response | Promise<Response>;

/**
 * JSON のレスポンスを作る。
 *
 * @param status ステータスコード。
 * @param body JSON にする値。
 * @param headers 追加のヘッダ。
 * @returns レスポンス。
 */
export function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });
}

/**
 * エラーのレスポンスを、API の形式で作る。
 *
 * @param status ステータスコード。
 * @param code エラーの種類。
 * @param message メッセージ。
 * @param headers 追加のヘッダ。
 * @returns レスポンス。
 */
export function errorResponse(status: number, code: string, message: string, headers: Record<string, string> = {}): Response {
  return jsonResponse(status, { error: { code, message } }, headers);
}

/**
 * 偽の fetch を作る。リクエストを、順に、記録する。
 *
 * @param handler 応答を決める関数。
 * @returns 偽の fetch と、記録された呼び出しの一覧。
 */
export function createFakeFetch(handler: Handler): { fetchImpl: typeof fetch; requests: RecordedRequest[] } {
  const requests: RecordedRequest[] = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    // リクエストの内容を、読みやすい形にして、記録する。
    const headers = Object.fromEntries(Object.entries((init?.headers ?? {}) as Record<string, string>));
    const recorded: RecordedRequest = {
      method: init?.method ?? "GET",
      path: typeof input === "string" ? input : input instanceof URL ? input.pathname + input.search : input.url,
      headers,
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
    };
    requests.push(recorded);
    return handler(recorded);
  };
  return { fetchImpl, requests };
}

/**
 * メモリだけの、localStorage の代わり。
 *
 * @param initial 最初に入れておく値。
 * @returns Storage の一部の操作を持つオブジェクト。
 */
export function createMemoryStorage(initial: Record<string, string> = {}): Pick<Storage, "getItem" | "setItem" | "removeItem"> {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
  };
}

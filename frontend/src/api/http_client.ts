// fetch の薄い包み。トークンの付与、401 のときの更新と1回だけの再試行、エラーの変換を行う。
import { ApiError, parseRetryAfter, readErrorBody } from "./api_error";

/** HttpClient が、トークンを得るために使うもの(実体は AuthSession)。 */
export interface TokenSource {
  /** いまの access token を返す(無ければ null)。 */
  getAccessToken(): string | null;

  /**
   * access token を更新して、新しいものを返す。refresh token が使えなければ null(このとき、ログアウトの状態になる)。
   * 接続の失敗など、一時的なエラーのときは、例外を投げる(ログアウトにしない)。
   */
  refreshAccessToken(): Promise<string | null>;
}

/** 1回のリクエストの指定。 */
export interface RequestOptions {
  /** JSON にして送るボディ。 */
  body?: unknown;

  /** false なら、Authorization を付けず、401 でも更新しない(ログイン・サインアップ用)。既定は true。 */
  authenticated?: boolean;

  /** 追加のヘッダ(refresh token を Authorization に入れるときなど)。 */
  headers?: Record<string, string>;

  /** 中断の合図。 */
  signal?: AbortSignal;
}

/** API を呼ぶ道具。パスは、`/api/v1` や `/auth` を含む、サイトの絶対パス。 */
export class HttpClient {
  private readonly tokens: TokenSource;
  private readonly fetchImpl: typeof fetch;

  /**
   * HttpClient を作る。
   *
   * @param tokens トークンの取得元。
   * @param fetchImpl 使う fetch(テストで差し替える)。
   */
  constructor(tokens: TokenSource, fetchImpl: typeof fetch = (...args) => fetch(...args)) {
    this.tokens = tokens;
    this.fetchImpl = fetchImpl;
  }

  /**
   * API を呼んで、JSON のボディを返す。
   *
   * 認証つきで 401 が返ったときは、access token を更新して、1回だけ、やり直す。更新できなければ、401 のエラーを投げる。
   *
   * @param method HTTP のメソッド。
   * @param path サイトの絶対パス(例: /api/v1/projects)。
   * @param options リクエストの指定。
   * @returns レスポンスの JSON(ボディが空なら、空のオブジェクト)。
   * @throws ApiError ステータスが 2xx でないとき、または、接続できなかったとき。
   */
  async request<T>(method: string, path: string, options: RequestOptions = {}): Promise<T> {
    // 認証つきがすでに既定。
    const authenticated = options.authenticated ?? true;
    // 1回目の呼び出し。
    const first = await this.send(method, path, options, authenticated ? this.tokens.getAccessToken() : null);
    // 401 以外、または認証なしの呼び出しは、そのまま結果にする。
    if (first.status !== 401 || !authenticated) {
      return this.unwrap<T>(first);
    }
    // 401 のときは、access token を更新して、1回だけやり直す(更新に失敗したら、元の 401 を返す)。
    const refreshed = await this.tokens.refreshAccessToken();
    if (refreshed === null) {
      return this.unwrap<T>(first);
    }
    return this.unwrap<T>(await this.send(method, path, options, refreshed));
  }

  /**
   * 1回だけ送る(エラーの変換は、unwrap が行う)。
   *
   * @param method HTTP のメソッド。
   * @param path パス。
   * @param options リクエストの指定。
   * @param accessToken 付ける access token(無ければ付けない)。
   * @returns レスポンス。
   * @throws ApiError 接続できなかったとき(network_error)。
   */
  private async send(method: string, path: string, options: RequestOptions, accessToken: string | null): Promise<Response> {
    // ヘッダを組み立てる(ボディがあれば JSON、トークンがあれば Authorization)。
    const headers: Record<string, string> = { ...options.headers };
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    if (accessToken !== null && headers["Authorization"] === undefined) {
      headers["Authorization"] = `Bearer ${accessToken}`;
    }
    try {
      // 送る。
      return await this.fetchImpl(path, {
        method,
        headers,
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        signal: options.signal ?? null,
      });
    } catch (cause) {
      // 中断(画面を離れたなど)は、そのまま伝える。
      if (cause instanceof DOMException && cause.name === "AbortError") {
        throw cause;
      }
      // それ以外は、接続できなかったこととして扱う。
      throw new ApiError(0, "network_error", "サーバーに接続できません");
    }
  }

  /**
   * レスポンスを、JSON か、ApiError にする。
   *
   * @param response レスポンス。
   * @returns JSON のボディ。
   * @throws ApiError ステータスが 2xx でないとき。
   */
  private async unwrap<T>(response: Response): Promise<T> {
    // ボディを、JSON として読む(空や、JSON でないものは、undefined)。
    const text = await response.text();
    let body: unknown;
    try {
      body = text === "" ? undefined : JSON.parse(text);
    } catch {
      body = undefined;
    }
    // 2xx なら、ボディを返す。
    if (response.ok) {
      return (body ?? {}) as T;
    }
    // それ以外は、エラーにする。
    const { code, message } = readErrorBody(response.status, body);
    throw new ApiError(response.status, code, message, parseRetryAfter(response.headers.get("Retry-After")));
  }
}

// ログインの状態(ログイン、サインアップ、ログアウト、access token の更新)を管理する。React に依存しない。
import { ApiError } from "../api/api_error";
import { HttpClient, type TokenSource } from "../api/http_client";
import type { CurrentUser } from "../api/types";
import { REFRESH_TOKEN_KEY, type TokenStore } from "./token_store";

/** ログインの状態。loading は、保存してある refresh token で、確かめている最中。 */
export type SessionState = { status: "loading" } | { status: "anonymous" } | { status: "authenticated"; user: CurrentUser };

/** ログインの状態を持つ。HttpClient に、access token を渡す(TokenSource)。 */
export class AuthSession implements TokenSource {
  /** 認証つきの API を呼ぶ道具。 */
  readonly http: HttpClient;

  private readonly store: TokenStore;
  private state: SessionState = { status: "loading" };
  private accessToken: string | null = null;
  private refreshToken: string | null;
  private refreshInFlight: Promise<string | null> | null = null;
  private readonly listeners = new Set<() => void>();

  /**
   * AuthSession を作る。
   *
   * @param store refresh token の保存先。
   * @param fetchImpl 使う fetch(テストで差し替える)。
   */
  constructor(store: TokenStore, fetchImpl?: typeof fetch) {
    this.store = store;
    // 保存してある refresh token を読む(あれば、restore で、確かめる)。
    this.refreshToken = store.read();
    this.http = new HttpClient(this, fetchImpl);
  }

  /** 状態の変化を受け取る(React の useSyncExternalStore 用)。解除する関数を返す。 */
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  /** いまの状態を返す(変わらない限り、同じ参照を返す)。 */
  getSnapshot = (): SessionState => this.state;

  /** HttpClient 用: いまの access token を返す。 */
  getAccessToken(): string | null {
    return this.accessToken;
  }

  /**
   * 状態を変えて、受け取り手に知らせる。
   *
   * @param next 新しい状態。
   */
  private setState(next: SessionState): void {
    this.state = next;
    // 全ての受け取り手に知らせる。
    this.listeners.forEach((listener) => listener());
  }

  /** ログアウトの状態にする(保存してある refresh token も消す)。 */
  private clearSession(): void {
    this.accessToken = null;
    this.refreshToken = null;
    this.store.clear();
    this.setState({ status: "anonymous" });
  }

  /**
   * 保存してある refresh token で、ログインの状態を戻す(アプリの起動時に1回呼ぶ)。
   *
   * refresh token が無い・使えない(401)なら、ログアウトの状態になる。接続の失敗のときは、refresh token を残して、
   * ログアウトの状態にする(リロードで、やり直せる)。
   */
  async restore(): Promise<void> {
    // refresh token が無ければ、ログインしていない。
    if (this.refreshToken === null) {
      this.setState({ status: "anonymous" });
      return;
    }
    try {
      // access token を取って、ユーザーを確かめる。
      if ((await this.refreshAccessToken()) === null) {
        return;
      }
      await this.loadUser();
    } catch {
      // 一時的な失敗: refresh token は残して、ログイン画面を出す。
      this.accessToken = null;
      this.setState({ status: "anonymous" });
    }
  }

  /**
   * 他のタブでの、ログイン・ログアウトを、このタブに反映する(storage イベント)。
   *
   * @param event ブラウザの storage イベント。
   */
  handleStorageEvent = (event: StorageEvent): void => {
    // refresh token の変化だけを見る。
    if (event.key !== REFRESH_TOKEN_KEY && event.key !== null) {
      return;
    }
    // 他のタブで消された(ログアウトした)なら、このタブも、ログアウトにする。
    if (this.store.read() === null && this.refreshToken !== null) {
      this.accessToken = null;
      this.refreshToken = null;
      this.setState({ status: "anonymous" });
    }
  };

  /**
   * ユーザー名とパスワードで、ログインする。
   *
   * @param userName ユーザー名。
   * @param password パスワード。
   * @param turnstileToken 人間であることの確認(Cloudflare Turnstile)のトークン。確認が有効なときだけ要る(無ければ送らない)。
   * @throws ApiError 認証できない(401)、確認に失敗(400)、レート制限(429)、接続の失敗など。
   */
  async login(userName: string, password: string, turnstileToken?: string): Promise<void> {
    // トークンは、あるときだけ送る。refresh token を受け取って、ログインの状態にする。
    const body: Record<string, string> = { userName, password };
    if (turnstileToken !== undefined && turnstileToken !== "") {
      body["turnstileToken"] = turnstileToken;
    }
    const response = await this.http.request<{ token: string }>("POST", "/auth/login", { body, authenticated: false });
    await this.startSession(response.token);
  }

  /**
   * ユーザーを作って、そのままログインする。
   *
   * @param userName ユーザー名。
   * @param password パスワード。
   * @param invitationCode 招待コード(招待制のときだけ要る。空なら送らない)。
   * @param turnstileToken 人間であることの確認(Cloudflare Turnstile)のトークン。確認が有効なときだけ要る(無ければ送らない)。
   * @throws ApiError 入力の不正・確認に失敗(400)、名前の重複(409)、レート制限(429)など。
   */
  async signup(userName: string, password: string, invitationCode: string, turnstileToken?: string): Promise<void> {
    // 招待コードとトークンは、あるときだけ送る。
    const body: Record<string, string> = { userName, password };
    if (invitationCode.trim() !== "") {
      body["invitationCode"] = invitationCode.trim();
    }
    if (turnstileToken !== undefined && turnstileToken !== "") {
      body["turnstileToken"] = turnstileToken;
    }
    const response = await this.http.request<{ token: string }>("POST", "/auth/signup", { body, authenticated: false });
    await this.startSession(response.token);
  }

  /**
   * 受け取った refresh token で、ログインの状態にする。
   *
   * @param refreshToken refresh token。
   */
  private async startSession(refreshToken: string): Promise<void> {
    // 保存して、access token とユーザーを取る。失敗したら、ログアウトに戻す。
    this.refreshToken = refreshToken;
    this.store.write(refreshToken);
    try {
      if ((await this.refreshAccessToken()) === null) {
        throw new ApiError(401, "unauthorized", "ログインできませんでした");
      }
      await this.loadUser();
    } catch (cause) {
      this.clearSession();
      throw cause;
    }
  }

  /** access token を使って、ログインしているユーザーを取り、ログインの状態にする。 */
  private async loadUser(): Promise<void> {
    const user = await this.http.request<{ userId: string; userName: string }>("GET", "/auth/me");
    this.setState({ status: "authenticated", user: { userId: user.userId, userName: user.userName } });
  }

  /**
   * ログアウトする。サーバーの refresh token も無効にする(失敗しても、この端末は、ログアウトにする)。
   */
  async logout(): Promise<void> {
    const refreshToken = this.refreshToken;
    // 先に、この端末を、ログアウトにする(サーバーが遅くても、画面は、すぐ戻る)。
    this.clearSession();
    if (refreshToken === null) {
      return;
    }
    try {
      // サーバーの refresh token を、無効にする(Authorization に、refresh token を入れる)。
      await this.http.request<unknown>("POST", "/auth/logout", { authenticated: false, headers: { Authorization: `Bearer ${refreshToken}` } });
    } catch {
      // 失敗しても、できることは無い(refresh token は、90 日で切れる)。
    }
  }

  /**
   * access token を更新する。同時に何回呼ばれても、サーバーへの更新は、1回だけ行う。
   *
   * @returns 新しい access token。refresh token が使えなければ null(ログアウトの状態になる)。
   * @throws ApiError 接続の失敗など、一時的なエラーのとき(ログアウトにはしない)。
   */
  refreshAccessToken(): Promise<string | null> {
    // すでに更新中なら、その結果を待つ。
    if (this.refreshInFlight !== null) {
      return this.refreshInFlight;
    }
    this.refreshInFlight = this.doRefresh().finally(() => {
      this.refreshInFlight = null;
    });
    return this.refreshInFlight;
  }

  /**
   * サーバーに、access token の更新を頼む。
   *
   * @returns 新しい access token。使えなければ null。
   */
  private async doRefresh(): Promise<string | null> {
    // 他のタブが、新しい refresh token で、ログインし直したかもしれないので、保存してあるものを、読み直す。
    this.refreshToken = this.store.read() ?? this.refreshToken;
    if (this.refreshToken === null) {
      this.clearSession();
      return null;
    }
    try {
      const response = await this.http.request<{ token: string }>("POST", "/auth/refresh", {
        authenticated: false,
        headers: { Authorization: `Bearer ${this.refreshToken}` },
      });
      this.accessToken = response.token;
      return response.token;
    } catch (cause) {
      // refresh token が、使えない(ログアウト済み、期限切れ、停止された)なら、ログアウトにする。
      if (cause instanceof ApiError && cause.status === 401) {
        this.clearSession();
        return null;
      }
      throw cause;
    }
  }
}

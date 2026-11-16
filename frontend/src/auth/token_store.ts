// refresh token の保存。ブラウザの localStorage に置く(access token は、メモリだけに置き、保存しない)。
//
// localStorage は、ページ内のスクリプト(XSS)から読めるので、画面の部品は、外のスクリプトや、
// 信頼できない HTML を、読み込まない(依存の少ない SPA にしている)。auth が Cookie に対応したら、HttpOnly Cookie に移す。

/** refresh token を保存する場所の名前。 */
export const REFRESH_TOKEN_KEY = "launchs.refreshToken";

/** refresh token の保存と取り出し。保存先が使えないとき(プライベートウィンドウなど)も、例外を投げない。 */
export class TokenStore {
  private readonly storage: Pick<Storage, "getItem" | "setItem" | "removeItem"> | null;

  /**
   * TokenStore を作る。
   *
   * @param storage 保存先(ブラウザの localStorage。テストで差し替える。使えなければ null)。
   */
  constructor(storage: Pick<Storage, "getItem" | "setItem" | "removeItem"> | null) {
    this.storage = storage;
  }

  /**
   * 保存してある refresh token を返す。
   *
   * @returns refresh token。無い、または読めなければ null。
   */
  read(): string | null {
    try {
      // 保存先から読む。
      return this.storage?.getItem(REFRESH_TOKEN_KEY) ?? null;
    } catch {
      // 保存先が使えないときは、無いものとして扱う。
      return null;
    }
  }

  /**
   * refresh token を保存する。
   *
   * @param token refresh token。
   */
  write(token: string): void {
    try {
      this.storage?.setItem(REFRESH_TOKEN_KEY, token);
    } catch {
      // 保存できなくても、このタブの中では、メモリの値で動く(リロードで、ログインし直しになる)。
    }
  }

  /** 保存してある refresh token を消す。 */
  clear(): void {
    try {
      this.storage?.removeItem(REFRESH_TOKEN_KEY);
    } catch {
      // 消せなくても、できることは無い。
    }
  }
}

/**
 * ブラウザの localStorage を、安全に取り出す(アクセスするだけで例外になる環境がある)。
 *
 * @returns localStorage。使えなければ null。
 */
export function browserStorage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

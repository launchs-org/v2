// Cloudflare Turnstile(人間であることの確認)のウィジェットを、画面に出すための、読み込みと型。
//
// スクリプトは、Cloudflare のもの(https://challenges.cloudflare.com)。本番の静的配信は、確認を使うときだけ、
// CSP でこのオリジンを許す(frontend/security-headers.inc.template、環境変数 CSP_TURNSTILE)。

/** ウィジェットの設定(使うものだけ)。 */
export interface TurnstileRenderOptions {
  sitekey: string;
  theme?: "auto" | "light" | "dark";
  language?: string;
  size?: "normal" | "flexible" | "compact";
  callback: (token: string) => void;
  "expired-callback": () => void;
  "error-callback": () => void;
}

/** Cloudflare のスクリプトが、window に置く、操作の入口(使うものだけ)。 */
export interface TurnstileApi {
  render(container: HTMLElement, options: TurnstileRenderOptions): string;
  reset(widgetId: string): void;
  remove(widgetId: string): void;
}

declare global {
  interface Window {
    turnstile?: TurnstileApi;
  }
}

/** Cloudflare のスクリプトの URL(ウィジェットは、明示的に描くので、render=explicit)。 */
export const TURNSTILE_SCRIPT_URL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

/** スクリプトの読み込みを、1回だけ行うための、約束。 */
let scriptPromise: Promise<TurnstileApi> | null = null;

/**
 * Turnstile のスクリプトを読み込む(何度呼んでも、読み込みは1回)。すでに window.turnstile があれば、それを使う。
 *
 * @returns Turnstile の入口。読み込めなければ、失敗する(失敗した後に呼ぶと、もう一度試す)。
 */
export function loadTurnstile(): Promise<TurnstileApi> {
  // すでに使えるなら、読み込まない。
  if (window.turnstile !== undefined) {
    return Promise.resolve(window.turnstile);
  }
  // 読み込み中なら、その結果を待つ。
  if (scriptPromise !== null) {
    return scriptPromise;
  }
  scriptPromise = new Promise<TurnstileApi>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = TURNSTILE_SCRIPT_URL;
    script.async = true;
    script.onload = () => {
      // 読み込めても、入口が無ければ、失敗とする。
      if (window.turnstile === undefined) {
        scriptPromise = null;
        reject(new Error("Turnstile のスクリプトが、使える状態になりませんでした"));
        return;
      }
      resolve(window.turnstile);
    };
    script.onerror = () => {
      // 失敗したら、次に呼ばれたときに、やり直せるようにする。
      scriptPromise = null;
      script.remove();
      reject(new Error("Turnstile のスクリプトを読み込めませんでした"));
    };
    document.head.appendChild(script);
  });
  return scriptPromise;
}

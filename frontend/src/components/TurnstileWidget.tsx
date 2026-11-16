// Cloudflare Turnstile のウィジェット(人間であることの確認)。フォームの下に置いて、トークンを受け取る。
import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { loadTurnstile } from "../auth/turnstile";
import { Alert } from "./ui/alert";

/** 親から、ウィジェットを、やり直す(トークンを作り直す)ための操作。 */
export interface TurnstileHandle {
  /** トークンは、1回しか使えない。送信した後(成功しても、失敗しても)に呼んで、新しいトークンを作らせる。 */
  reset(): void;
}

/**
 * ウィジェット。トークンができると onToken に渡し、期限切れ・エラーのときは、null を渡す。
 *
 * @param props.siteKey Cloudflare のサイトキー。
 * @param props.onToken トークン(無効になったら null)を受け取る。
 * @param props.handle 親が、reset を呼ぶための入口。
 */
export function TurnstileWidget({ siteKey, onToken, handle }: { siteKey: string; onToken: (token: string | null) => void; handle?: Ref<TurnstileHandle> }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const widgetIdRef = useRef<string | null>(null);
  const onTokenRef = useRef(onToken);
  const [loadFailed, setLoadFailed] = useState(false);
  // 最新の onToken を、描画のたびに、覚えておく(ウィジェットを描き直さずに、使うため)。
  onTokenRef.current = onToken;

  useImperativeHandle(handle, () => ({
    reset() {
      // トークンを無効にして、ウィジェットに、新しいトークンを作らせる。
      onTokenRef.current(null);
      if (widgetIdRef.current !== null) {
        window.turnstile?.reset(widgetIdRef.current);
      }
    },
  }), []);

  useEffect(() => {
    // スクリプトを読み込んで、コンテナにウィジェットを描く。画面を離れたら(または、キーが変わったら)、取り除く。
    let cancelled = false;
    setLoadFailed(false);
    loadTurnstile()
      .then((turnstile) => {
        if (cancelled || containerRef.current === null) {
          return;
        }
        widgetIdRef.current = turnstile.render(containerRef.current, {
          sitekey: siteKey,
          theme: "auto",
          language: "ja",
          size: "flexible",
          callback: (token) => onTokenRef.current(token),
          "expired-callback": () => onTokenRef.current(null),
          "error-callback": () => onTokenRef.current(null),
        });
      })
      .catch(() => {
        if (!cancelled) {
          setLoadFailed(true);
        }
      });
    return () => {
      cancelled = true;
      if (widgetIdRef.current !== null) {
        window.turnstile?.remove(widgetIdRef.current);
        widgetIdRef.current = null;
      }
    };
  }, [siteKey]);

  return (
    <div className="flex flex-col gap-2">
      <div ref={containerRef} data-testid="turnstile-container" className="min-h-[65px]" />
      {loadFailed && <Alert>人間であることの確認の部品を、読み込めませんでした。ネットワークや、広告ブロッカーを確認して、ページを読み込み直してください。</Alert>}
    </div>
  );
}

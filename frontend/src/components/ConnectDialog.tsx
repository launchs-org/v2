// 線を引いてつなぐときの、入力のダイアログ(標準の dialog 要素)。
import { Link2 } from "lucide-react";
import { useEffect, useRef, type FormEvent, type ReactNode } from "react";
import { ErrorMessage } from "./ErrorMessage";
import { Button } from "./ui/button";
import { Spinner } from "./ui/spinner";

/**
 * つなぐ内容を入力するダイアログ。open が true の間、表示する。
 *
 * @param props.open 表示するか。
 * @param props.title 見出し。
 * @param props.description 説明。
 * @param props.submitLabel 送信ボタンの文。
 * @param props.canSubmit 送信できるか(つなげない理由があるときは false)。
 * @param props.busy 送信中か。
 * @param props.error 送信の失敗(あれば表示する)。
 * @param props.onSubmit 送信したとき。
 * @param props.onCancel やめるとき。
 * @param props.children 入力欄。
 */
export function ConnectDialog(props: { open: boolean; title: string; description: string; submitLabel: string; canSubmit: boolean; busy: boolean; error?: unknown; onSubmit: () => void; onCancel: () => void; children: ReactNode }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    // open の変化に合わせて開閉する。showModal に対応しない環境(jsdom など)では、open 属性で開閉する。
    const dialog = dialogRef.current;
    if (dialog === null) {
      return;
    }
    if (typeof dialog.showModal !== "function") {
      dialog.toggleAttribute("open", props.open);
      return;
    }
    if (props.open && !dialog.open) {
      dialog.showModal();
    } else if (!props.open && dialog.open) {
      dialog.close();
    }
  }, [props.open]);

  /**
   * フォームが送信されたとき: ページの遷移を止めて、送信を伝える。
   *
   * @param event フォームの送信イベント。
   */
  function handleSubmit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    props.onSubmit();
  }

  return (
    <dialog ref={dialogRef} onCancel={props.onCancel} aria-labelledby="connect-title" className="m-auto w-[calc(100%-2rem)] max-w-md rounded-xl border bg-card p-0 text-card-foreground shadow-lg backdrop:bg-black/50">
      {props.open && (
        <form className="flex flex-col gap-4 p-6" onSubmit={handleSubmit}>
          <div className="flex items-start gap-4">
            <span className="grid size-10 shrink-0 place-items-center rounded-full bg-primary/10 text-primary"><Link2 aria-hidden="true" className="size-5" /></span>
            <div className="min-w-0">
              <h2 id="connect-title" className="text-lg font-semibold">{props.title}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{props.description}</p>
            </div>
          </div>
          {props.children}
          <ErrorMessage error={props.error} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={props.onCancel} disabled={props.busy}>やめる</Button>
            <Button type="submit" disabled={!props.canSubmit || props.busy}>
              {props.busy && <Spinner />}
              {props.submitLabel}
            </Button>
          </div>
        </form>
      )}
    </dialog>
  );
}

// 取り返しのつかない操作の、確認のダイアログ(標準の dialog 要素。Esc で閉じ、フォーカスが閉じ込められ、背面は操作できない)。
// 標準の要素を使うのは、外部の部品が、インラインのスタイルを足して、厳しい CSP に違反するのを避けるため。
import { TriangleAlert } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";
import { Button } from "./ui/button";
import { Spinner } from "./ui/spinner";

/**
 * 確認のダイアログ。open が true の間、表示する。
 *
 * @param props.open 表示するか。
 * @param props.title 見出し。
 * @param props.confirmLabel 実行するボタンの文。
 * @param props.busy 実行中か(実行中は、ボタンを押せない)。
 * @param props.onConfirm 実行するとき。
 * @param props.onCancel やめるとき。
 * @param props.children 説明。
 */
export function ConfirmDialog(props: { open: boolean; title: string; confirmLabel: string; busy: boolean; onConfirm: () => void; onCancel: () => void; children: ReactNode }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    // open の変化に合わせて、ダイアログを開閉する。showModal に対応しない環境(jsdom など)では、open 属性で開閉する。
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
  return (
    <dialog
      ref={dialogRef}
      onCancel={props.onCancel}
      aria-labelledby="confirm-title"
      className="m-auto w-[calc(100%-2rem)] max-w-md rounded-2xl border bg-card p-0 text-card-foreground shadow-xl backdrop:bg-black/50 open:animate-in open:fade-in-0 open:zoom-in-95"
    >
      {props.open && (
        <div className="p-6">
          <div className="flex items-start gap-4">
            <span className="grid size-10 shrink-0 place-items-center rounded-full bg-destructive/12 text-destructive">
              <TriangleAlert aria-hidden="true" className="size-5" />
            </span>
            <div className="min-w-0">
              <h2 id="confirm-title" className="text-lg font-medium">{props.title}</h2>
              <div className="mt-2 text-sm text-muted-foreground">{props.children}</div>
            </div>
          </div>
          <div className="mt-6 flex justify-end gap-2">
            <Button variant="ghost" onClick={props.onCancel} disabled={props.busy}>やめる</Button>
            <Button variant="destructive" onClick={props.onConfirm} disabled={props.busy}>
              {props.busy && <Spinner />}
              {props.confirmLabel}
            </Button>
          </div>
        </div>
      )}
    </dialog>
  );
}

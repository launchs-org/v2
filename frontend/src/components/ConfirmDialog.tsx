// 取り返しのつかない操作の、確認のダイアログ(標準の dialog 要素。Esc で閉じ、フォーカスが閉じ込められる)。
import { useEffect, useRef, type ReactNode } from "react";

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
    <dialog ref={dialogRef} className="dialog" onCancel={props.onCancel} aria-labelledby="confirm-title">
      {props.open && (
        <>
          <h2 id="confirm-title">{props.title}</h2>
          <div>{props.children}</div>
          <div className="actions">
            <button type="button" className="button button--ghost" onClick={props.onCancel} disabled={props.busy}>やめる</button>
            <button type="button" className="button button--danger" onClick={props.onConfirm} disabled={props.busy}>{props.confirmLabel}</button>
          </div>
        </>
      )}
    </dialog>
  );
}

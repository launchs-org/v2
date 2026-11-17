// 「適用前の変更(pending)」がある設定の、適用と破棄のボタン(config・マウント・サービス・ingress・volume で共通)。
import { Check, Undo2 } from "lucide-react";
import { ErrorMessage } from "./ErrorMessage";
import { Alert } from "./ui/alert";
import { Button } from "./ui/button";
import { Spinner } from "./ui/spinner";

/**
 * 適用前の変更があることを知らせ、適用と破棄を頼むボタンを出す。
 *
 * @param props.description 適用すると何が起きるか(例: 「コンテナが再起動します」)。
 * @param props.disabled 操作できない間(反映中など)は true。
 * @param props.applying 適用を頼んでいる最中か。
 * @param props.discarding 破棄を頼んでいる最中か。
 * @param props.onApply 適用するとき。
 * @param props.onDiscard 破棄するとき。
 * @param props.error 適用・破棄の失敗(あれば表示する)。
 */
export function PendingActions(props: { description: string; disabled: boolean; applying: boolean; discarding: boolean; onApply: () => void; onDiscard: () => void; error?: unknown }) {
  return (
    <Alert variant="info" className="flex flex-col gap-3">
      <p>適用前の変更があります。{props.description}</p>
      <div className="flex gap-2">
        <Button size="sm" onClick={props.onApply} disabled={props.disabled || props.applying || props.discarding}>
          {props.applying ? <Spinner /> : <Check aria-hidden="true" />}
          適用
        </Button>
        <Button size="sm" variant="outline" onClick={props.onDiscard} disabled={props.disabled || props.applying || props.discarding}>
          {props.discarding ? <Spinner /> : <Undo2 aria-hidden="true" />}
          破棄
        </Button>
      </div>
      <ErrorMessage error={props.error} />
    </Alert>
  );
}

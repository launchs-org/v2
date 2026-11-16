// エラーの表示(画面の読み上げにも伝わる)。
import { describeError } from "../api/api_error";
import { Alert } from "./ui/alert";

/**
 * エラーを、画面に出す。
 *
 * @param props.error 捕まえたエラー。無ければ、何も出さない。
 * @param props.className 追加のクラス。
 */
export function ErrorMessage({ error, className }: { error: unknown; className?: string }) {
  if (error === null || error === undefined) {
    return null;
  }
  return <Alert className={className}>{describeError(error)}</Alert>;
}

// エラーの表示(画面の読み上げにも伝わる)。
import { describeError } from "../api/api_error";

/**
 * エラーを、画面に出す。
 *
 * @param props.error 捕まえたエラー。無ければ、何も出さない。
 */
export function ErrorMessage({ error }: { error: unknown }) {
  if (error === null || error === undefined) {
    return null;
  }
  return <p className="error" role="alert">{describeError(error)}</p>;
}

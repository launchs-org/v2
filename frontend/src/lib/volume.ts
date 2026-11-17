// volume の状態の表示と、入力の検査。
import type { StatusTone } from "./project_name";
import { validateProjectName } from "./project_name";

/** volume の既定の大きさ(MB)。作成フォームの初期値。 */
export const DEFAULT_VOLUME_SIZE_MB = 1024;

/**
 * volume の名前を検査する(規則は、プロジェクトの名前と同じ。英数字とハイフンのみ、1〜64文字)。
 *
 * @param name 入力された名前(前後の空白は、呼び出し側で除く)。
 * @returns 問題の説明。合っていれば null。
 */
export function validateVolumeName(name: string): string | null {
  // プロジェクト名と同じ規則なので、その検査を使う(メッセージの「名前」は、そのまま通じる)。
  return validateProjectName(name);
}

/**
 * volume の大きさ(MB)の入力を検査する。
 *
 * @param input 入力された文字列。
 * @param maxSizeMB 上限(MB)。
 * @param currentSizeMB 拡大のときだけ、いまの大きさ(縮小を断る)。作成のときは undefined。
 * @returns 問題の説明。合っていれば null。
 */
export function validateVolumeSize(input: string, maxSizeMB: number, currentSizeMB?: number): string | null {
  // 数字だけの正の整数でなければ断る。
  if (!/^[0-9]+$/.test(input.trim()) || Number(input) < 1) {
    return "大きさは、1以上の整数(MB)で入力してください。";
  }
  const sizeMB = Number(input);
  // 上限を超えるものは断る。
  if (sizeMB > maxSizeMB) {
    return `大きさは、${maxSizeMB} MB 以下にしてください。`;
  }
  // 拡大のとき、いまより小さい(縮小)ものは断る。
  if (currentSizeMB !== undefined && sizeMB < currentSizeMB) {
    return `縮小はできません。${currentSizeMB} MB 以上にしてください。`;
  }
  return null;
}

/**
 * volume の状態を、画面に出す言葉にする。
 *
 * @param status volume の状態。
 * @returns 表示する言葉(知らない値は、そのまま)。
 */
export function volumeStatusLabel(status: string): string {
  // 仕様の4つの状態を、日本語にする。
  switch (status) {
    case "active":
      return "利用可能";
    case "applying":
      return "拡張中";
    case "failed":
      return "拡張に失敗";
    case "deleting":
      return "削除中";
    default:
      return status;
  }
}

/**
 * volume の状態を、バッジの色にする。
 *
 * @param status volume の状態。
 * @returns 色の種類。
 */
export function volumeStatusTone(status: string): StatusTone {
  // 進行中は黄、失敗は赤、利用可能は緑。
  switch (status) {
    case "active":
      return "success";
    case "applying":
    case "deleting":
      return "warning";
    case "failed":
      return "destructive";
    default:
      return "default";
  }
}
